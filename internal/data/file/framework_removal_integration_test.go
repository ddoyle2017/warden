package file_test

import (
	"database/sql"
	"errors"
	"github.com/ddoyle2017/warden/internal/data/file"
	"github.com/ddoyle2017/warden/internal/data/repo"
	"github.com/ddoyle2017/warden/internal/domain/framework"
	"github.com/ddoyle2017/warden/internal/domain/mod"
	"github.com/ddoyle2017/warden/internal/service"
	"github.com/ddoyle2017/warden/internal/test/helper"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattn/go-sqlite3"
)

func TestFrameworkRemovalFailureStates(t *testing.T) {
	for name, tt := range map[string]struct {
		removalFailure, restoreFailure, nestedCompletion, cleanupFailure bool
		sqlAction                                                        string
		wantFiles, wantRecords                                           bool
	}{
		"given installed plugins, when removal completes, then remove their files and metadata": {},
		"given partial file removal, when recovery succeeds, then retain the original files and metadata": {
			removalFailure: true, wantFiles: true, wantRecords: true,
		},
		"given partial file removal and failed recovery, when returning, then retain metadata and the original snapshot": {
			removalFailure: true, restoreFailure: true, wantRecords: true,
		},
		"given a nested completion marker in a failed removal, when recovering, then retain files and metadata": {
			removalFailure: true, nestedCompletion: true, wantFiles: true, wantRecords: true,
		},
		"given completed removal with cleanup failure, when reconciling metadata, then delete records and report the artifact": {
			cleanupFailure: true,
		},
		"given rejected metadata deletion after file removal, when returning, then report the mismatch and preserve both tables": {
			sqlAction: "FAIL", wantRecords: true,
		},
		"given cleanup and metadata failures after file removal, when returning, then preserve both diagnostics": {
			cleanupFailure: true, sqlAction: "FAIL", wantRecords: true,
		},
		"given a database rollback diagnostic after file removal, when returning, then request database verification": {
			sqlAction: "ROLLBACK", wantRecords: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			server := th.GetValheimDirectory()
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			db := th.CreateDatabase().(*sql.DB)
			repo.CreateFrameworksTable(db)
			repo.CreateModsTable(db)
			fr, mr := repo.NewFrameworksRepo(db), repo.NewModsRepo(db)
			if err := fr.InsertFramework(framework.Framework{Name: framework.BepInEx, Version: "1.0.0"}); err != nil {
				t.Fatal(err)
			}
			originalFramework, err := fr.GetFramework(framework.BepInEx)
			if err != nil {
				t.Fatal(err)
			}
			mods := []mod.Mod{
				{Name: "Associated", Namespace: "Owner", Version: "1.0.0", FrameworkID: originalFramework.ID},
				{Name: "Legacy", Namespace: "Owner", Version: "1.0.0"},
			}
			originalFiles := map[string]string{
				"BepInEx/core/runtime":    "original framework",
				"doorstop_libs/loader":    "original loader",
				"start_server_bepinex.sh": "framework startup",
				"start_server.sh":         "vanilla startup",
				"world.db":                "unrelated world",
			}
			for i := range mods {
				relative := filepath.Join("BepInEx", "plugins", mods[i].FullName())
				mods[i].FilePath = filepath.Join(server, relative)
				originalFiles[filepath.Join(relative, "plugin.dll")] = mods[i].Name + " bytes"
				if err := mr.InsertMod(mods[i]); err != nil {
					t.Fatal(err)
				}
				mods[i], err = mr.GetMod(mods[i].Name)
				if err != nil {
					t.Fatal(err)
				}
			}
			for name, contents := range originalFiles {
				path := filepath.Join(server, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.sqlAction != "" {
				trigger := `CREATE TRIGGER reject_framework_delete BEFORE DELETE ON frameworks
					BEGIN SELECT RAISE(` + tt.sqlAction + `, 'metadata deletion interrupted'); END`
				if _, err := db.Exec(trigger); err != nil {
					t.Fatal(err)
				}
			}
			removalCause := errors.New("framework removal interrupted")
			restoreCause := errors.New("server restoration interrupted")
			cleanupCause := errors.New("backup cleanup interrupted")
			b := file.NewBackup()
			var artifact string
			backup := &frameworkRemovalBackup{
				Backup: b,
				restore: func(destination string) error {
					if tt.restoreFailure {
						return errors.Join(file.ErrBackupRestoreFailed, restoreCause)
					}
					return b.Restore(destination)
				},
				remove: func() error {
					if !tt.cleanupFailure {
						return b.Remove()
					}
					return file.RemoveBackupWithCleanup(b, func(path string) error {
						if b.Path() != nil {
							t.Fatal("cleanup started before consuming the snapshot")
						}
						artifact = path
						if err := os.Remove(filepath.Join(path, "world.db")); err != nil {
							t.Fatal(err)
						}
						return cleanupCause
					})
				},
			}
			manager := file.NewManager(&mock.HTTPClient{}, server)
			removalCalls := 0
			fm := &mock.Manager{
				BepInExPluginDirectoryFunc: manager.BepInExPluginDirectory,
				RemoveBepInExFunc: func() error {
					removalCalls++
					return file.RemoveBepInExWithOperations(manager, backup, func(path string) error {
						if tt.removalFailure && path == filepath.Join(server, "doorstop_libs") {
							if _, err := os.Lstat(filepath.Join(server, "BepInEx")); !errors.Is(err, os.ErrNotExist) {
								t.Fatalf("fault did not follow partial removal: %v", err)
							}
							if tt.nestedCompletion {
								return errors.Join(removalCause, &file.FrameworkMutationCleanupError{Err: cleanupCause})
							}
							return removalCause
						}
						return os.RemoveAll(path)
					})
				},
			}
			svc := service.NewFrameworkService(fr, fm, &mock.Thunderstore{}, strings.NewReader("YES I AM\n"))
			err = svc.RemoveBepInEx()
			wantError := tt.removalFailure || tt.cleanupFailure || tt.sqlAction != ""
			if wantError {
				if !errors.Is(err, service.ErrUnableToRemoveFramework) {
					t.Errorf("removal diagnostic lost: %v", err)
				}
			} else if err != nil {
				t.Errorf("successful removal returned an error: %v", err)
			}
			if removalCalls != 1 {
				t.Errorf("file removal calls = %d, want 1", removalCalls)
			}
			if tt.removalFailure && (!errors.Is(err, removalCause) || !errors.Is(err, file.ErrFrameworkDeleteFailed)) {
				t.Errorf("incomplete-removal cause lost: %v", err)
			}
			if tt.restoreFailure && (!errors.Is(err, restoreCause) || !errors.Is(err, file.ErrBackupRestoreFailed)) {
				t.Errorf("recovery cause lost: %v", err)
			}
			var completed *file.FrameworkMutationCleanupError
			if errors.As(err, &completed) != (tt.cleanupFailure || tt.nestedCompletion) {
				t.Errorf("completion diagnostic lost or invented: %v", err)
			}
			if tt.sqlAction != "" {
				var cause sqlite3.Error
				if !errors.Is(err, repo.ErrFrameworkDeleteFailed) || !errors.As(err, &cause) ||
					cause.ExtendedCode != sqlite3.ErrConstraintTrigger ||
					!strings.Contains(err.Error(), "framework files were removed") ||
					!strings.Contains(err.Error(), "verify database state") {
					t.Errorf("metadata failure or recovery guidance lost: %v", err)
				}
				if tt.sqlAction == "ROLLBACK" && !strings.Contains(err.Error(), "rollback transaction") {
					t.Errorf("rollback diagnostic lost: %v", err)
				}
			}
			wantFiles := map[string]string{"start_server.sh": "vanilla startup", "world.db": "unrelated world"}
			if tt.wantFiles {
				wantFiles = originalFiles
			} else if tt.restoreFailure {
				wantFiles["doorstop_libs/loader"] = "original loader"
				wantFiles["start_server_bepinex.sh"] = "framework startup"
			}
			assertPluginFiles(t, th, server, wantFiles)
			for _, m := range mods {
				actual, err := mr.GetMod(m.Name)
				if tt.wantRecords {
					if err != nil || !actual.Equals(&m) {
						t.Errorf("record changed after failure: %+v, %v; want %+v", actual, err, m)
					}
				} else if !errors.Is(err, repo.ErrModFetchNoResults) {
					t.Errorf("removed plugin still recorded: %+v, %v", actual, err)
				}
			}
			actualFramework, fetchErr := fr.GetFramework(framework.BepInEx)
			if tt.wantRecords {
				if fetchErr != nil || actualFramework != originalFramework {
					t.Errorf("framework changed after failure: %+v, %v", actualFramework, fetchErr)
				}
			} else if !errors.Is(fetchErr, repo.ErrFrameworkFetchNoResults) {
				t.Errorf("removed framework still recorded: %+v, %v", actualFramework, fetchErr)
			}
			if !tt.restoreFailure && b.Path() != nil {
				t.Errorf("completed removal or recovery retained an active snapshot: %q", *b.Path())
			}
			entries, readErr := os.ReadDir(backups)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tt.restoreFailure {
				path := b.Path()
				if path == nil {
					t.Fatal("failed recovery lost the original snapshot")
				}
				if !strings.Contains(err.Error(), *path) || len(entries) != 1 || entries[0].Name() != filepath.Base(*path) {
					t.Errorf("retained snapshot is not correctly reported: path=%q, entries=%v, error=%v", *path, entries, err)
				}
				assertPluginFiles(t, th, *path, originalFiles)
			} else if tt.cleanupFailure {
				if !errors.Is(err, cleanupCause) || !errors.Is(err, file.ErrBackupDeleteFailed) ||
					artifact == "" || !strings.Contains(err.Error(), artifact) {
					t.Errorf("cleanup diagnostic or leftover path lost: %v", err)
				}
				if len(entries) != 1 || entries[0].Name() != filepath.Base(artifact) {
					t.Errorf("unexpected cleanup artifacts: %v", entries)
				}
				leftovers := make(map[string]string, len(originalFiles))
				for name, contents := range originalFiles {
					if name != "world.db" {
						leftovers[name] = contents
					}
				}
				assertPluginFiles(t, th, artifact, leftovers)
				if err := b.Restore(server); !errors.Is(err, file.ErrBackupMissing) {
					t.Errorf("partial cleanup artifact was offered for restoration: %v", err)
				}
			} else if len(entries) != 0 {
				t.Errorf("removal or recovery orphaned backups: %v", entries)
			}
		})
	}
}

type frameworkRemovalBackup struct {
	file.Backup
	remove  func() error
	restore func(string) error
}

func (b *frameworkRemovalBackup) Remove() error { return b.remove() }

func (b *frameworkRemovalBackup) Restore(destination string) error { return b.restore(destination) }
