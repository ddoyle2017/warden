package file_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestMissingOriginalCleanupWarning(t *testing.T) {
	for _, operation := range []string{"single update", "qualified update", "bulk update"} {
		for _, outcome := range []string{"confirmed", "rejected", "committed without acknowledgement"} {
			t.Run("given empty-staging cleanup fails, when "+operation+" recording is "+outcome+", then preserve file completion and metadata uncertainty", func(t *testing.T) {
				th := helper.NewHelper(t)
				server := th.GetValheimDirectory()
				t.Setenv("TMPDIR", t.TempDir())
				db := th.CreateDatabase()
				repo.CreateFrameworksTable(db)
				repo.CreateModsTable(db)
				stored := repo.NewModsRepo(db)
				original := mod.Mod{
					Namespace: "Owner", Name: "Root", Version: "1.0.0", Description: "original metadata",
					FilePath: filepath.Join(server, "BepInEx", "plugins", "Owner-Root-1.0.0"),
				}
				if err := stored.InsertMod(original); err != nil {
					t.Fatal(err)
				}
				original, err := stored.GetModByIdentity("Owner", "Root")
				if err != nil {
					t.Fatal(err)
				}
				latest := thunderstore.Release{
					Namespace: "Owner", Name: "Root", VersionNumber: "2.0.0",
					FullName: "Owner-Root-2.0.0", DownloadURL: "fixture",
				}
				manager := file.NewManager(&mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
					if url != latest.DownloadURL {
						t.Fatalf("unexpected download %q", url)
					}
					return &http.Response{Body: io.NopCloser(bytes.NewReader(recoveryArchive(t, false, false)))}, nil
				}}, server)
				cleanupCause, recordCause := errors.New("empty staging removal failed"), errors.New("record outcome unavailable")
				var staging string
				var replacement file.ModReplacement
				cleanups, writes, commits := 0, 0, 0
				files := &mock.Manager{
					ValidateModInstallFunc: manager.ValidateModInstall,
					ReplaceModFunc: func(url, oldName, newName string) (file.ModReplacement, error) {
						var err error
						replacement, err = file.ReplaceModWithOperations(manager, url, oldName, newName, os.Rename, os.Remove, func(path string) error {
							if writes != 0 {
								t.Fatal("empty staging was retained until after metadata handling")
							}
							staging = path
							cleanups++
							if entries, err := os.ReadDir(path); err != nil || len(entries) != 0 {
								t.Fatalf("cleanup tried to discard nonempty recovery: %v, %v", entries, err)
							}
							return cleanupCause
						})
						if replacement == nil || replacement.RecoveryPath() != "" {
							t.Fatalf("cleanup warning lost publication or invented original recovery: %v, %v", replacement, err)
						}
						if _, completed := err.(*file.ModInstallCleanupError); !completed || !errors.Is(err, cleanupCause) {
							t.Fatalf("empty-staging cleanup did not return direct file completion: %v", err)
						}
						return &mock.ModReplacement{
							PathFunc: replacement.Path, RecoveryPathFunc: replacement.RecoveryPath,
							CommitFunc: func() error {
								if outcome != "confirmed" || writes != 1 {
									t.Fatal("unconfirmed recording finalized its handle")
								}
								commits++
								return replacement.Commit()
							},
						}, err
					},
				}
				records := &mock.ModsRepo{
					GetModFunc: stored.GetMod, GetModByIdentityFunc: stored.GetModByIdentity, ListModsFunc: stored.ListMods,
					UpsertModFunc: func(m mod.Mod) error {
						writes++
						if outcome == "rejected" {
							return recordCause
						}
						if err := stored.UpsertMod(m); err != nil {
							t.Fatal(err)
						}
						if outcome == "committed without acknowledgement" {
							return recordCause
						}
						return nil
					},
				}
				catalog := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					if namespace != original.Namespace || name != original.Name {
						t.Fatalf("unexpected package %q/%q", namespace, name)
					}
					return thunderstore.Package{Latest: latest}, nil
				}}
				result := runModInstallRequest(service.NewModService(records, files, catalog, strings.NewReader("Y\n")), operation)
				if _, completed := result.(*service.ModMutationCleanupError); completed != (outcome == "confirmed") {
					t.Errorf("incorrect public completion: %v", result)
				}
				if !errors.Is(result, cleanupCause) || !errors.Is(result, file.ErrBackupDeleteFailed) || staging == "" || !strings.Contains(result.Error(), staging) {
					t.Errorf("empty staging path or cleanup cause lost: %v", result)
				}
				wantCommits := 0
				wantRecord := original
				if outcome != "rejected" {
					wantRecord.Version, wantRecord.Description = latest.VersionNumber, latest.Description
					wantRecord.FilePath = replacement.Path()
				}
				if outcome == "confirmed" {
					wantCommits = 1
				} else if !errors.Is(result, recordCause) || !errors.Is(result, service.ErrUnableToUpdateMod) ||
					!strings.Contains(result.Error(), "verify database state") || !strings.Contains(result.Error(), "original files were absent") {
					t.Errorf("metadata failure was masked by file completion: %v", result)
				}
				if cleanups != 1 || writes != 1 || commits != wantCommits {
					t.Errorf("cleanup/write/commit counts = %d/%d/%d, want 1/1/%d", cleanups, writes, commits, wantCommits)
				}
				if got, err := stored.GetModByIdentity("Owner", "Root"); err != nil || !got.Equals(&wantRecord) {
					t.Errorf("unexpected complete record: %+v, %v; want %+v", got, err, wantRecord)
				}
				assertArchiveRecoveryFiles(t, th, replacement.Path(), map[string]string{"plugin.dll": "replacement plugin"}, nil)
				if entries, err := os.ReadDir(staging); err != nil || len(entries) != 0 {
					t.Errorf("reported empty leftovers changed: %v, %v", entries, err)
				}
				if replacement.RecoveryPath() != "" || strings.Contains(result.Error(), "original files retained at") {
					t.Errorf("empty leftovers advertised as usable recovery: %v", result)
				}
			})
		}
	}
}
