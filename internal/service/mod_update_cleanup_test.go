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
		all       bool
		qualified bool
		removed   bool
		failure   string
	}{
		"given committed removal with a cleanup warning, when updating one mod, then install and record its replacement": {removed: true},
		"given committed removal with a cleanup warning, when updating all mods, then install and record replacements":   {all: true, removed: true},
		"given committed removal with a cleanup warning, when updating a qualified mod, then install and record its replacement": {
			qualified: true, removed: true,
		},
		"given failed removal, when updating one mod, then keep the old files and record":                  {},
		"given failed removal, when updating all mods, then keep the old files and record":                 {all: true},
		"given a nested removal marker, when updating one mod, then keep the old files and record":         {failure: "nested"},
		"given a nested removal marker, when updating a qualified mod, then keep the old files and record": {qualified: true, failure: "nested"},
		"given a nested removal marker, when updating all mods, then keep the old files and record":        {all: true, failure: "nested"},
		"given a removal cleanup warning, when installation fails, then retain both diagnostics and delete only the obsolete record": {
			removed: true, failure: "install",
		},
		"given a removal cleanup warning, when metadata fails, then retain both diagnostics without deleting the record": {
			removed: true, failure: "record",
		},
		"given removal and installation cleanup warnings, when updating, then finish metadata and retain both warnings": {
			removed: true, failure: "cleanup",
		},
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
			cleanupCause, installCause, recordCause := errors.New("retained cleanup directory"), errors.New("installation interrupted"), errors.New("metadata interrupted")
			r := &mock.ModsRepo{
				GetModFunc: func(string) (mod.Mod, error) { return record, nil },
				GetModByIdentityFunc: func(namespace, name string) (mod.Mod, error) {
					if namespace != record.Namespace || name != record.Name {
						t.Fatalf("unexpected qualified lookup: %s/%s", namespace, name)
					}
					return record, nil
				},
				ListModsFunc: func() ([]mod.Mod, error) { return []mod.Mod{record}, nil },
				UpsertModFunc: func(m mod.Mod) error {
					if !tt.removed || tt.failure == "install" {
						t.Fatal("incomplete replacement attempted metadata write")
					}
					if tt.failure == "record" {
						return recordCause
					}
					record = m
					return nil
				},
				DeleteModFunc: func(name, namespace string) error {
					if tt.failure != "install" || name != "Installed" || namespace != "Owned" {
						t.Fatalf("unexpected record cleanup: %s/%s", namespace, name)
					}
					record = mod.Mod{}
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
						if tt.failure == "nested" {
							return errors.Join(file.ErrModDeleteFailed, &file.ModRemovalCleanupError{Err: cleanupCause})
						}
						return file.ErrModDeleteFailed
					}
					if err := os.RemoveAll(oldPath); err != nil {
						t.Fatal(err)
					}
					return &file.ModRemovalCleanupError{Err: fmt.Errorf("%w: %w", file.ErrModDeleteFailed, cleanupCause)}
				},
				InstallModFunc: func(url, fullName string) (string, error) {
					if !tt.removed {
						t.Fatal("incomplete removal attempted replacement")
					}
					if url != latest.DownloadURL || fullName != latest.FullName {
						t.Fatalf("wrong replacement requested: %q, %q", url, fullName)
					}
					if tt.failure == "install" {
						return "", installCause
					}
					if err := os.MkdirAll(newPath, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(newPath, "plugin.dll"), []byte("new bytes"), 0o600); err != nil {
						t.Fatal(err)
					}
					if tt.failure == "cleanup" {
						return newPath, &file.ModInstallCleanupError{Err: installCause}
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
			} else if tt.qualified {
				err = ms.UpdateModByIdentity(record.Namespace, record.Name)
			} else {
				err = ms.UpdateMod(record.Name)
			}
			wantPath, wantVersion, wantBytes := oldPath, "1.0.0", "old bytes"
			missingPath := newPath
			if _, completed := err.(*service.ModMutationCleanupError); completed != (tt.removed && (tt.failure == "" || tt.failure == "cleanup")) {
				t.Errorf("whole-request completion misclassified: %v", err)
			}
			if !errors.Is(err, file.ErrModDeleteFailed) {
				t.Errorf("removal sentinel lost: %v", err)
			}
			if (tt.removed || tt.failure == "nested") && !errors.Is(err, cleanupCause) {
				t.Errorf("removal cleanup diagnostic lost: %v", err)
			}
			if tt.failure == "cleanup" && !errors.Is(err, installCause) {
				t.Errorf("installation cleanup diagnostic lost: %v", err)
			}
			if tt.failure == "install" {
				if !errors.Is(err, installCause) || !errors.Is(err, service.ErrUnableToUpdateMod) || !record.Equals(&mod.Mod{}) {
					t.Errorf("failed replacement lost a cause or retained obsolete metadata: record=%+v, error=%v", record, err)
				}
				for _, path := range []string{oldPath, newPath} {
					if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("failed replacement left unexpected release %q: %v", path, err)
					}
				}
				return
			}
			if tt.removed {
				wantPath, wantVersion, wantBytes = newPath, "2.0.0", "new bytes"
				missingPath = oldPath
				if tt.failure == "record" {
					if !errors.Is(err, recordCause) || !errors.Is(err, service.ErrUnableToUpdateMod) {
						t.Errorf("record failure diagnostic lost: %v", err)
					}
					wantVersion = "1.0.0"
				}
			} else if !errors.Is(err, service.ErrUnableToUpdateMod) {
				t.Errorf("expected failed-removal error, got %v", err)
			}
			wantRecordPath := wantPath
			if tt.failure == "record" {
				wantRecordPath = oldPath
			}
			if record.Version != wantVersion || record.FilePath != wantRecordPath {
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
