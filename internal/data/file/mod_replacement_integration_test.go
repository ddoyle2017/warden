package file_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestModReplacementFailurePreservesRecord(t *testing.T) {
	for _, operation := range []string{"single update", "qualified update", "bulk update"} {
		for _, phase := range []string{"retain original", "publication", "recovery", "restored cleanup", "archive cleanup"} {
			t.Run("given installed files and metadata, when "+operation+" fails during "+phase+", then retain both and all failure causes", func(t *testing.T) {
				th := helper.NewHelper(t)
				server := th.GetValheimDirectory()
				t.Setenv("TMPDIR", t.TempDir())
				db := th.CreateDatabase()
				repo.CreateFrameworksTable(db)
				repo.CreateModsTable(db)
				stored := repo.NewModsRepo(db)
				plugins := filepath.Join(server, "BepInEx", "plugins")
				original := mod.Mod{
					Namespace: "Owner", Name: "Root", Version: "1.0.0", FrameworkID: 17,
					FilePath: filepath.Join(plugins, "Owner-Root-1.0.0"), Description: "original description",
					WebsiteURL: "https://example.invalid/original",
				}
				writeArchiveRecoveryFiles(t, original.FilePath, map[string]string{"plugin.dll": "original DLL", "nested/settings": "original settings"})
				writeArchiveRecoveryFiles(t, plugins, map[string]string{"untracked.dll": "untracked DLL"})
				if err := os.Chmod(original.FilePath, 0o750); err != nil {
					t.Fatal(err)
				}
				if err := stored.InsertMod(original); err != nil {
					t.Fatal(err)
				}
				original, err := stored.GetModByIdentity("Owner", "Root")
				if err != nil {
					t.Fatal(err)
				}
				latest := thunderstore.Release{
					Namespace: "Owner", Name: "Root", VersionNumber: "2.0.0", FullName: "Owner-Root-2.0.0",
					DownloadURL: "fixture", WebsiteURL: "https://example.invalid/new", Description: "new description",
				}
				destination := filepath.Join(plugins, latest.FullName)
				manager := file.NewManager(&mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
					if url != latest.DownloadURL {
						t.Fatalf("unexpected download %q", url)
					}
					return &http.Response{Body: io.NopCloser(bytes.NewReader(recoveryArchive(t, false, false)))}, nil
				}}, server)
				primary, recoveryCause, cleanupCause := errors.New("replacement interrupted"), errors.New("original recovery interrupted"), errors.New("cleanup interrupted")
				var staging, recovery string
				fm := &mock.Manager{
					ValidateModInstallFunc: manager.ValidateModInstall,
					ReplaceModFunc: func(url, oldFullName, newFullName string) (file.ModReplacement, error) {
						if url != latest.DownloadURL || oldFullName != original.FullName() || newFullName != latest.FullName {
							t.Fatalf("unexpected replacement %q, %q, %q", url, oldFullName, newFullName)
						}
						return file.ReplaceModWithOperations(manager, url, oldFullName, newFullName, func(source, target string) error {
							switch {
							case source == original.FilePath:
								recovery = target
								if phase == "retain original" {
									return errors.Join(primary, syscall.EXDEV)
								}
							case target == destination:
								return errors.Join(primary, syscall.EXDEV)
							case target == original.FilePath:
								if phase == "recovery" {
									return recoveryCause
								}
							default:
								t.Fatalf("unexpected rename %q -> %q", source, target)
							}
							return os.Rename(source, target)
						}, func(path string) error {
							staging = filepath.Dir(path)
							if phase == "archive cleanup" {
								return primary
							}
							return os.Remove(path)
						}, func(path string) error {
							if phase == "recovery" {
								t.Fatal("failed recovery deleted the retained original")
							}
							if path != staging {
								t.Fatalf("cleanup escaped its staging: %q", path)
							}
							if phase == "restored cleanup" {
								return cleanupCause
							}
							return os.RemoveAll(path)
						})
					},
				}
				r := &mock.ModsRepo{
					GetModFunc: stored.GetMod, GetModByIdentityFunc: stored.GetModByIdentity, ListModsFunc: stored.ListMods,
					UpsertModFunc: func(mod.Mod) error {
						t.Fatal("incomplete replacement wrote metadata")
						return nil
					},
					DeleteModFunc: func(string, string) error {
						t.Fatal("incomplete replacement deleted the original record")
						return nil
					},
				}
				ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					if namespace != "Owner" || name != "Root" {
						t.Fatalf("unexpected package %q/%q", namespace, name)
					}
					return thunderstore.Package{Latest: latest}, nil
				}}
				err = runModInstallRequest(service.NewModService(r, fm, ts, strings.NewReader("Y\n")), operation)
				if !errors.Is(err, service.ErrUnableToUpdateMod) || !errors.Is(err, primary) {
					t.Fatalf("replacement cause lost: %v", err)
				}
				if phase != "archive cleanup" && !errors.Is(err, syscall.EXDEV) {
					t.Errorf("cross-device rename cause was lost: %v", err)
				}
				if _, completed := err.(*service.ModMutationCleanupError); completed {
					t.Errorf("failed replacement reported completion: %v", err)
				}
				if errors.Is(err, file.ErrBackupRestoreFailed) != (phase == "recovery") {
					t.Errorf("actual recovery outcome lost: %v", err)
				}
				want := map[string]string{"untracked.dll": "untracked DLL"}
				root := original.FilePath
				if phase == "recovery" {
					root = recovery
					if !errors.Is(err, recoveryCause) || !strings.Contains(err.Error(), root) {
						t.Errorf("retained original or recovery cause lost: %v", err)
					}
				} else {
					want[original.FullName()+"/plugin.dll"] = "original DLL"
					want[original.FullName()+"/nested/settings"] = "original settings"
				}
				assertArchiveRecoveryFiles(t, th, root, map[string]string{"plugin.dll": "original DLL", "nested/settings": "original settings"}, nil)
				assertArchiveRecoveryFiles(t, th, plugins, want, nil)
				if info, err := os.Stat(root); err != nil || info.Mode().Perm() != 0o750 {
					t.Errorf("original mode changed: %v, %v", info, err)
				}
				if phase == "restored cleanup" && (!errors.Is(err, cleanupCause) || !errors.Is(err, file.ErrBackupDeleteFailed) || !strings.Contains(err.Error(), staging)) {
					t.Errorf("successful restoration lost its secondary cleanup error: %v", err)
				}
				actual, err := stored.ListMods()
				if err != nil || !reflect.DeepEqual(actual, []mod.Mod{original}) {
					t.Errorf("original metadata changed: %+v, %v", actual, err)
				}
			})
		}
	}
}
