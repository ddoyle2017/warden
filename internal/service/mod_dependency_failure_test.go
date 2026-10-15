package service_test

import (
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

func TestDependencyInstall_BackupFailurePreservesExistingRecord(t *testing.T) {
	tests := map[string]string{
		"given an existing current dependency, when its backup fails, then preserve its files and record": "1.0.0",
		"given an existing older dependency, when its backup fails, then preserve its files and record":   "2.0.0",
	}
	for name, latestVersion := range tests {
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
			dependencyRequested := false
			client := &mock.HTTPClient{
				GetFunc: func(url string) (*http.Response, error) {
					if url == "dependency-download" {
						dependencyRequested = true
						if err := os.Symlink(filepath.Join(plugins, "missing"), filepath.Join(plugins, "b-dangling")); err != nil {
							t.Fatal(err)
						}
					} else if url != "root-download" {
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
						release.Dependencies = []string{"Owner-Dependency-" + latestVersion}
					case "Dependency":
						release.VersionNumber = latestVersion
						release.FullName = "Owner-Dependency-" + latestVersion
						release.DownloadURL = "dependency-download"
					default:
						t.Fatalf("unexpected package %q", name)
					}
					return thunderstore.Package{Latest: release}, nil
				},
			}
			ms := service.NewModService(mr, file.NewManager(client, th.GetValheimDirectory()), ts, strings.NewReader(""))
			err = ms.AddMod("Owner", "Root")
			if !dependencyRequested || !errors.Is(err, service.ErrAddDependenciesFailed) {
				t.Fatalf("expected a dependency failure after root installation: requested=%v, error=%v", dependencyRequested, err)
			}
			if _, err := mr.GetMod("Root"); err != nil {
				t.Errorf("root installation record was lost: %v", err)
			}
			if data, err := os.ReadFile(payload); err != nil || string(data) != "existing dependency bytes" {
				t.Errorf("dependency bytes changed during backup preflight: %q, %v", data, err)
			}
			after, err := mr.GetMod(dependency.Name)
			if err != nil || after.ID != before.ID || !after.Equals(&before) {
				t.Errorf("pre-existing dependency record changed: before=%+v after=%+v error=%v", before, after, err)
			}
		})
	}
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
