package service_test

import (
	"errors"
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

func TestUpdate_RecordCleanupOwnership(t *testing.T) {
	tests := map[string]struct {
		failure     string
		wantVersion string
		wantOldFile bool
		wantNewFile bool
	}{
		"given failed old-file removal, when updating, then preserve its record and files": {
			failure: "remove", wantVersion: "1.0.0", wantOldFile: true,
		},
		"given removed old files, when replacement fails, then remove the obsolete record": {
			failure: "install",
		},
		"given replacement files, when recording fails, then do not compensate by deleting the record": {
			failure: "record", wantVersion: "1.0.0", wantNewFile: true,
		},
		"given failed replacement and record cleanup, when returning, then retain both error causes": {
			failure: "cleanup", wantVersion: "1.0.0",
		},
		"given a successful replacement, when recording it, then retain the new files and record": {
			wantVersion: "2.0.0", wantNewFile: true,
		},
	}
	for name, tt := range tests {
		for _, all := range []bool{false, true} {
			mode := "single"
			if all {
				mode = "all"
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				th := helper.NewHelper(t)
				db := th.CreateDatabase()
				repo.CreateFrameworksTable(db)
				repo.CreateModsTable(db)
				th.SeedFrameworksTable(repo.NewFrameworksRepo(db))
				mr := repo.NewModsRepo(db)
				current := mod.Mod{Namespace: "Owner", Name: "Installed", Version: "1.0.0", FrameworkID: 1}
				plugins := filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory)
				oldPath := filepath.Join(plugins, current.FullName())
				current.FilePath = oldPath
				if err := os.MkdirAll(oldPath, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(oldPath, "plugin.dll"), []byte("old bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := mr.InsertMod(current); err != nil {
					t.Fatal(err)
				}
				latest := thunderstore.Release{
					Namespace: current.Namespace, Name: current.Name, VersionNumber: "2.0.0",
					FullName: "Owner-Installed-2.0.0", DownloadURL: helper.TestDownloadURL,
				}
				newPath := filepath.Join(plugins, latest.FullName)
				installFailure := errors.New("replacement installation failed")
				recordFailure := errors.New("record write failed")
				cleanupFailure := errors.New("obsolete record cleanup failed")
				r := &mock.ModsRepo{
					GetModFunc:   mr.GetMod,
					ListModsFunc: mr.ListMods,
					UpsertModFunc: func(m mod.Mod) error {
						if tt.failure == "record" {
							return recordFailure
						}
						return mr.UpsertMod(m)
					},
					DeleteModFunc: func(name, namespace string) error {
						if name != current.Name || namespace != current.Namespace {
							t.Fatalf("cleanup targeted a different record: %s/%s", namespace, name)
						}
						if tt.failure == "cleanup" {
							return cleanupFailure
						}
						return mr.DeleteMod(name, namespace)
					},
				}
				fm := &mock.Manager{
					RemoveModFunc: func(fullName string) error {
						if fullName != current.FullName() {
							t.Fatalf("removed a different release: %s", fullName)
						}
						if tt.failure == "remove" {
							return file.ErrModDeleteFailed
						}
						return os.RemoveAll(oldPath)
					},
					InstallModFunc: func(url, fullName string) (string, error) {
						if url != latest.DownloadURL || fullName != latest.FullName {
							t.Fatalf("installed a different release: %q/%q", url, fullName)
						}
						if tt.failure == "install" || tt.failure == "cleanup" {
							return "", installFailure
						}
						if err := os.MkdirAll(newPath, 0o700); err != nil {
							return "", err
						}
						if err := os.WriteFile(filepath.Join(newPath, "plugin.dll"), []byte("new bytes"), 0o600); err != nil {
							return "", err
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
				if all {
					err = ms.UpdateAllMods()
				} else {
					err = ms.UpdateMod(current.Name)
				}
				if tt.failure == "" {
					if err != nil {
						t.Fatalf("replacement failed: %v", err)
					}
				} else if !errors.Is(err, service.ErrUnableToUpdateMod) {
					t.Fatalf("expected an update failure, got %v", err)
				}
				if tt.failure == "cleanup" && (!errors.Is(err, installFailure) || !errors.Is(err, cleanupFailure)) {
					t.Errorf("update discarded a failure cause: %v", err)
				}
				record, recordErr := mr.GetMod(current.Name)
				if tt.wantVersion == "" {
					if !errors.Is(recordErr, repo.ErrModFetchNoResults) {
						t.Errorf("obsolete record remained after removed-file failure: %+v, %v", record, recordErr)
					}
				} else if recordErr != nil || record.Version != tt.wantVersion {
					t.Errorf("unexpected retained record: %+v, %v", record, recordErr)
				}
				for _, want := range []struct {
					path    string
					present bool
					bytes   string
				}{
					{oldPath, tt.wantOldFile, "old bytes"},
					{newPath, tt.wantNewFile, "new bytes"},
				} {
					data, err := os.ReadFile(filepath.Join(want.path, "plugin.dll"))
					if want.present {
						if err != nil || string(data) != want.bytes {
							t.Errorf("unexpected file contents at %s: %q, %v", want.path, data, err)
						}
					} else if !errors.Is(err, os.ErrNotExist) {
						t.Errorf("unexpected file at %s: %q, %v", want.path, data, err)
					}
				}
			})
		}
	}
}
