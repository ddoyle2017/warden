package service_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
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

func TestModUpdate_PreflightPreservesInstallation(t *testing.T) {
	tests := map[string]struct {
		inside  bool
		answer  string
		version string
		wantErr error
		updated bool
	}{
		"given cwd inside plugins, when replacement is refused, then preserve files and records": {
			inside: true, answer: "Y\n", version: "2.0.0", wantErr: file.ErrBackupRestoreFailed,
		},
		"given cwd outside plugins, when updating, then install and record the replacement": {
			answer: "Y\n", version: "2.0.0", updated: true,
		},
		"given cwd inside plugins, when no update is available, then leave the installation unchanged": {
			inside: true, answer: "Y\n", version: "1.0.0",
		},
		"given cwd inside plugins, when declining an update, then leave the installation unchanged": {
			inside: true, answer: "n\n", version: "2.0.0",
		},
		"given cwd inside plugins, when input is exhausted, then leave the installation unchanged": {
			inside: true, version: "2.0.0",
		},
		"given cwd inside plugins, when confirmation is invalid, then report it without mutation": {
			inside: true, answer: "invalid\ninvalid\ninvalid\n", version: "2.0.0", wantErr: service.ErrMaxAttempts,
		},
	}
	for name, tt := range tests {
		for _, all := range []bool{false, true} {
			mode := "single"
			if all {
				mode = "all"
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				th, mr, original, backups := newUpdatePreflightFixture(t)
				plugins := filepath.Dir(original.FilePath)
				if tt.inside {
					updatePreflightWorkingDirectory(t, plugins)
				}
				var payload bytes.Buffer
				archive := zip.NewWriter(&payload)
				entry, err := archive.Create("plugin.dll")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := io.WriteString(entry, "replacement bytes"); err != nil {
					t.Fatal(err)
				}
				if err := archive.Close(); err != nil {
					t.Fatal(err)
				}
				latest := thunderstore.Release{
					Namespace: original.Namespace, Name: original.Name, VersionNumber: tt.version,
					FullName: "Owned-Installed-" + tt.version, DownloadURL: helper.TestDownloadURL,
				}
				downloads := 0
				client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
					if url != latest.DownloadURL {
						t.Fatalf("unexpected download URL %q", url)
					}
					downloads++
					return &http.Response{Body: io.NopCloser(bytes.NewReader(payload.Bytes()))}, nil
				}}
				ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					if namespace != original.Namespace || name != original.Name {
						t.Fatalf("unexpected package %s/%s", namespace, name)
					}
					return thunderstore.Package{Latest: latest}, nil
				}}
				ms := service.NewModService(mr, file.NewManager(client, th.GetValheimDirectory()), ts, strings.NewReader(tt.answer))
				if all {
					err = ms.UpdateAllMods()
				} else {
					err = ms.UpdateMod(original.Name)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				if tt.wantErr == file.ErrBackupRestoreFailed && !errors.Is(err, service.ErrUnableToUpdateMod) {
					t.Errorf("refusal lost the public update sentinel: %v", err)
				}
				want, wantBytes, wantDownloads := original, "installed bytes", 0
				absentPath := filepath.Join(plugins, "Owned-Installed-2.0.0")
				if tt.updated {
					want.Version, want.FilePath = "2.0.0", absentPath
					wantBytes, wantDownloads, absentPath = "replacement bytes", 1, original.FilePath
				}
				record, recordErr := mr.GetMod(original.Name)
				if recordErr != nil || !reflect.DeepEqual(record, want) {
					t.Errorf("unexpected record: %+v, %v; want %+v", record, recordErr, want)
				}
				if data, err := os.ReadFile(filepath.Join(want.FilePath, "plugin.dll")); err != nil || string(data) != wantBytes {
					t.Errorf("unexpected installed bytes: %q, %v; want %q", data, err, wantBytes)
				}
				if _, err := os.Stat(absentPath); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("unexpected release directory %q: %v", absentPath, err)
				}
				if downloads != wantDownloads {
					t.Errorf("archive downloads = %d, want %d", downloads, wantDownloads)
				}
				if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
					t.Errorf("unexpected backup artifacts: %v, %v", entries, err)
				}
			})
		}
	}
}

func TestStandaloneModRemoval_FromPluginsWorkingDirectory(t *testing.T) {
	for _, all := range []bool{false, true} {
		name := "given cwd inside plugins, when removing one mod, then remove its files and record"
		if all {
			name = "given cwd inside plugins, when removing all mods, then remove files and records"
		}
		t.Run(name, func(t *testing.T) {
			th, mr, original, backups := newUpdatePreflightFixture(t)
			updatePreflightWorkingDirectory(t, filepath.Dir(original.FilePath))
			answer := "Y\n"
			if all {
				answer = "YES I AM\n"
			}
			ms := service.NewModService(mr, file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory()), &mock.Thunderstore{}, strings.NewReader(answer))
			var err error
			if all {
				err = ms.RemoveAllMods()
			} else {
				err = ms.RemoveMod(original.Namespace, original.Name)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := mr.GetMod(original.Name); !errors.Is(err, repo.ErrModFetchNoResults) {
				t.Errorf("standalone removal kept its record: %v", err)
			}
			if _, err := os.Stat(original.FilePath); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("standalone removal kept its files: %v", err)
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("standalone removal left backups: %v, %v", entries, err)
			}
		})
	}
}

func newUpdatePreflightFixture(t *testing.T) (helper.Helper, repo.Mods, mod.Mod, string) {
	t.Helper()
	th := helper.NewHelper(t)
	backups := t.TempDir()
	t.Setenv("TMPDIR", backups)
	db := th.CreateDatabase()
	repo.CreateFrameworksTable(db)
	repo.CreateModsTable(db)
	th.SeedFrameworksTable(repo.NewFrameworksRepo(db))
	mr := repo.NewModsRepo(db)
	current := mod.Mod{Name: "Installed", Namespace: "Owned", Version: "1.0.0", FrameworkID: 1}
	current.FilePath = filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory, current.FullName())
	if err := os.MkdirAll(current.FilePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current.FilePath, "plugin.dll"), []byte("installed bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mr.InsertMod(current); err != nil {
		t.Fatal(err)
	}
	record, err := mr.GetMod(current.Name)
	if err != nil {
		t.Fatal(err)
	}
	return th, mr, record, backups
}

func updatePreflightWorkingDirectory(t *testing.T, path string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore test working directory: %v", err)
		}
	})
}
