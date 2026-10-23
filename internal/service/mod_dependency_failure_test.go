package service_test

import (
	"bytes"
	"errors"
	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"github.com/ddoyle2017/warden/internal/data/file"
	"github.com/ddoyle2017/warden/internal/data/repo"
	"github.com/ddoyle2017/warden/internal/domain/mod"
	"github.com/ddoyle2017/warden/internal/service"
	"github.com/ddoyle2017/warden/internal/test/helper"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAddMod_PreservesRecordedDependencies(t *testing.T) {
	tests := map[string]string{
		"given a recorded dependency, when add declares the same version, then reuse its files and record": "1.0.0",
		"given a recorded dependency, when add declares a newer version, then do not silently upgrade it":  "2.0.0",
		"given a recorded dependency, when add declares an older version, then do not downgrade it":        "0.9.0",
	}
	for name, declaredVersion := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t, helper.WithDataFolder(filepath.Join("..", "test", "data")))
			t.Setenv("TMPDIR", t.TempDir())
			db := th.CreateDatabase()
			repo.CreateFrameworksTable(db)
			repo.CreateModsTable(db)
			th.SeedFrameworksTable(repo.NewFrameworksRepo(db))
			mr := repo.NewModsRepo(db)
			dependency := mod.Mod{
				Namespace: "Owner", Name: "Dependency", Version: "1.0.0", FrameworkID: 1,
				WebsiteURL: "https://example.invalid/installed", Description: "existing dependency metadata",
			}
			plugins := filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory)
			dependency.FilePath = filepath.Join(plugins, dependency.FullName())
			if err := os.MkdirAll(dependency.FilePath, 0o700); err != nil {
				t.Fatal(err)
			}
			payload := filepath.Join(dependency.FilePath, "plugin.dll")
			if err := os.WriteFile(payload, []byte("existing dependency bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := mr.InsertMod(dependency); err != nil {
				t.Fatal(err)
			}
			before, err := mr.GetMod(dependency.Name)
			if err != nil {
				t.Fatal(err)
			}
			downloads := 0
			client := &mock.HTTPClient{
				GetFunc: func(url string) (*http.Response, error) {
					downloads++
					if url != "root-download" {
						t.Fatalf("unexpected mock URL %q", url)
					}
					archive, err := os.Open(filepath.Join(th.GetDataDirectory(), helper.TestModFullName+file.ZipFileExtension))
					if err != nil {
						t.Fatal(err)
					}
					return &http.Response{StatusCode: http.StatusOK, Body: archive}, nil
				},
			}
			ts := &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					if namespace != "Owner" {
						t.Fatalf("unexpected namespace %q", namespace)
					}
					release := thunderstore.Release{Namespace: namespace, Name: name, VersionNumber: "1.0.0"}
					switch name {
					case "Root":
						release.FullName = "Owner-Root-1.0.0"
						release.DownloadURL = "root-download"
						release.Dependencies = []string{"Owner-Dependency-" + declaredVersion}
					default:
						t.Fatalf("unexpected package %q", name)
					}
					return thunderstore.Package{Latest: release}, nil
				},
			}
			ms := service.NewModService(mr, file.NewManager(client, th.GetValheimDirectory()), ts, strings.NewReader(""))
			err = ms.AddMod("Owner", "Root")
			if err != nil || downloads != 1 {
				t.Fatalf("expected only a root installation: downloads=%d, error=%v", downloads, err)
			}
			if _, err := mr.GetMod("Root"); err != nil {
				t.Errorf("root installation record was lost: %v", err)
			}
			if data, err := os.ReadFile(payload); err != nil || string(data) != "existing dependency bytes" {
				t.Errorf("recorded dependency bytes changed: %q, %v", data, err)
			}
			after, err := mr.GetMod(dependency.Name)
			if err != nil || after.ID != before.ID || !after.Equals(&before) {
				t.Errorf("pre-existing dependency record changed: before=%+v after=%+v error=%v", before, after, err)
			}
		})
	}
}

func TestUpdateMod_DependencyBackupFailurePreservesRecordedState(t *testing.T) {
	t.Run("given a recorded dependency, when its backup is refused during an update, then keep its complete state and the updated root", func(t *testing.T) {
		f := newModReplacementFixture(t)
		root := f.latest
		root.Dependencies = []string{"Other-Unrelated-3.0.0"}
		blockedTemp := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(blockedTemp, []byte("backup creation blocker"), 0o600); err != nil {
			t.Fatal(err)
		}
		var packages, downloads []string
		ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
			packages = append(packages, namespace+"/"+name)
			switch {
			case namespace == "Owned" && name == "Installed":
				return thunderstore.Package{Latest: root}, nil
			case namespace == "Other" && name == "Unrelated":
				return thunderstore.Package{Latest: thunderstore.Release{
					Namespace: "Other", Name: "Unrelated", VersionNumber: "4.0.0",
					FullName: "Other-Unrelated-4.0.0", DownloadURL: "dependency-download",
				}}, nil
			default:
				t.Fatalf("unexpected package %s/%s", namespace, name)
				return thunderstore.Package{}, nil
			}
		}}
		archive := replacementArchive(t, "success")
		client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
			downloads = append(downloads, url)
			switch url {
			case root.DownloadURL:
			case "dependency-download":
				t.Setenv("TMPDIR", blockedTemp)
			default:
				t.Fatalf("unexpected download %q", url)
			}
			return &http.Response{Body: io.NopCloser(bytes.NewReader(archive))}, nil
		}}
		err := service.NewModService(f.records, file.NewManager(client, f.server), ts, strings.NewReader("Y\n")).
			UpdateModByIdentity(f.original.Namespace, f.original.Name)
		if !errors.Is(err, service.ErrAddDependenciesFailed) || !errors.Is(err, file.ErrBackupCreateFailed) {
			t.Fatalf("expected dependency backup refusal, got %v", err)
		}
		var pathErr *os.PathError
		if !errors.As(err, &pathErr) || !strings.HasPrefix(pathErr.Path, blockedTemp+string(os.PathSeparator)) {
			t.Errorf("controlled backup creation cause was lost: %v", err)
		}
		if _, completed := err.(*service.ModMutationCleanupError); completed {
			t.Errorf("backup refusal reported whole-request completion: %v", err)
		}
		if !slices.Equal(packages, []string{"Owned/Installed", "Other/Unrelated"}) ||
			!slices.Equal(downloads, []string{root.DownloadURL, "dependency-download"}) {
			t.Errorf("failure was reached through the wrong package/download path: %v/%v", packages, downloads)
		}
		assertModReplacementState(t, f.server, f.records, f.updatedFiles(), []mod.Mod{f.updatedRecord(), f.other})
		f.assertNoArtifacts(t)
	})
}

func TestInitialInstallFailure_DoesNotDeleteRecords(t *testing.T) {
	r := &mock.ModsRepo{
		GetModByIdentityFunc: func(string, string) (mod.Mod, error) {
			return mod.Mod{}, repo.ErrModFetchNoResults
		},
		DeleteModFunc: func(string, string) error {
			t.Fatal("initial installation did not own a record to delete")
			return nil
		},
	}
	fm := &mock.Manager{
		InstallModFunc: func(string, string) (string, error) {
			return "", file.ErrBackupCreateFailed
		},
	}
	ts := &mock.Thunderstore{
		GetPackageFunc: func(string, string) (thunderstore.Package, error) {
			return thunderstore.Package{Latest: thunderstore.Release{Namespace: "Owner", Name: "New", FullName: "Owner-New-1.0.0"}}, nil
		},
	}
	ms := service.NewModService(r, fm, ts, &io.LimitedReader{})
	if err := ms.AddMod("Owner", "New"); !errors.Is(err, service.ErrModInstallFailed) {
		t.Errorf("expected the installation error, got %v", err)
	}
}
