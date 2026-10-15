package service_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestModUpdate_DistinguishesCommittedRemoval(t *testing.T) {
	tests := map[string]struct {
		all     bool
		removed bool
	}{
		"given committed removal with a cleanup warning, when updating one mod, then install and record its replacement": {removed: true},
		"given committed removal with a cleanup warning, when updating all mods, then install and record replacements":   {all: true, removed: true},
		"given failed removal, when updating one mod, then keep the old files and record":                                {},
		"given failed removal, when updating all mods, then keep the old files and record":                               {all: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			record := mod.Mod{Namespace: "Owned", Name: "Installed", Version: "1.0.0"}
			plugins := filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory)
			oldPath := filepath.Join(plugins, record.FullName())
			record.FilePath = oldPath
			if err := os.MkdirAll(oldPath, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(oldPath, "plugin.dll"), []byte("old bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			latest := thunderstore.Release{
				Namespace: "Owned", Name: "Installed", VersionNumber: "2.0.0",
				FullName: "Owned-Installed-2.0.0", DownloadURL: helper.TestDownloadURL,
			}
			newPath := filepath.Join(plugins, latest.FullName)
			r := &mock.ModsRepo{
				GetModFunc:   func(string) (mod.Mod, error) { return record, nil },
				ListModsFunc: func() ([]mod.Mod, error) { return []mod.Mod{record}, nil },
				UpsertModFunc: func(m mod.Mod) error {
					record = m
					return nil
				},
			}
			fm := &mock.Manager{
				ValidateModInstallFunc: func() error { return nil },
				RemoveModFunc: func(fullName string) error {
					if fullName != "Owned-Installed-1.0.0" {
						t.Fatalf("wrong release removed: %s", fullName)
					}
					if !tt.removed {
						return file.ErrModDeleteFailed
					}
					if err := os.RemoveAll(oldPath); err != nil {
						t.Fatal(err)
					}
					return fmt.Errorf("%w: %w", file.ErrModDeleteFailed, &file.ModRemovalCleanupError{Err: errors.New("retained cleanup directory")})
				},
				InstallModFunc: func(url, fullName string) (string, error) {
					if url != latest.DownloadURL || fullName != latest.FullName {
						t.Fatalf("wrong replacement requested: %q, %q", url, fullName)
					}
					if err := os.MkdirAll(newPath, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(newPath, "plugin.dll"), []byte("new bytes"), 0o600); err != nil {
						t.Fatal(err)
					}
					return newPath, nil
				},
			}
			ts := &mock.Thunderstore{
				GetPackageFunc: func(string, string) (thunderstore.Package, error) {
					return thunderstore.Package{Latest: latest}, nil
				},
			}
			ms := service.NewModService(r, fm, ts, strings.NewReader("Y"))
			var err error
			if tt.all {
				err = ms.UpdateAllMods()
			} else {
				err = ms.UpdateMod(record.Name)
			}
			wantPath, wantVersion, wantBytes := oldPath, "1.0.0", "old bytes"
			missingPath := newPath
			if tt.removed {
				wantPath, wantVersion, wantBytes = newPath, "2.0.0", "new bytes"
				missingPath = oldPath
				if err != nil {
					t.Errorf("cleanup diagnostic blocked replacement: %v", err)
				}
			} else if !errors.Is(err, service.ErrUnableToUpdateMod) {
				t.Errorf("expected failed-removal error, got %v", err)
			}
			if record.Version != wantVersion || record.FilePath != wantPath {
				t.Errorf("incorrect installed record: %+v", record)
			}
			if data, err := os.ReadFile(filepath.Join(wantPath, "plugin.dll")); err != nil || string(data) != wantBytes {
				t.Errorf("incorrect installed bytes: %q, %v", data, err)
			}
			if _, err := os.Stat(missingPath); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("unexpected release directory %s: %v", missingPath, err)
			}
		})
	}
}
