package file

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/api"
)

type frameworkBackup struct {
	Backup
	restore func(string) error
	remove  func() error
}

func (b *frameworkBackup) Restore(destination string) error { return b.restore(destination) }
func (b *frameworkBackup) Remove() error                    { return b.remove() }

func TestUpdateBepInExPartialRemoval(t *testing.T) {
	for name, restoreFails := range map[string]bool{
		"given partial framework removal, when updating fails, then restore the original server without downloading":     false,
		"given partial removal and failed restoration, when updating fails, then retain the original recovery snapshots": true,
	} {
		t.Run(name, func(t *testing.T) {
			parent, backups := restoreTempDir(t), t.TempDir()
			server := filepath.Join(parent, "server")
			t.Setenv("TMPDIR", backups)
			saved := map[string]string{
				"BepInEx/core/runtime":          "original framework",
				"BepInEx/config/user.cfg":       "user configuration",
				"BepInEx/plugins/user":          "original plugin",
				"BepInEx/plugins/nested/config": "plugin configuration",
				"doorstop_libs/runtime":         "original loader",
				"doorstop_config.ini":           "loader configuration",
				"server-data":                   "unrelated bytes",
			}
			partial := map[string]string{
				"doorstop_libs/runtime": "original loader",
				"doorstop_config.ini":   "loader configuration",
				"server-data":           "unrelated bytes",
			}
			writeRestoreTree(t, server, saved)
			b := NewBackup().(*backup)
			requests := 0
			m := NewManager(&pluginHTTPClient{GetFunc: func(string) (*http.Response, error) {
				requests++
				return nil, errors.New("unexpected download")
			}}, server).(*manager)
			removeErr, restoreErr := errors.New("framework removal interrupted"), errors.New("restoration interrupted")
			var snapshot, plugins string
			m.backup = &frameworkBackup{
				Backup: b, remove: b.Remove,
				restore: func(destination string) error {
					if destination != server || b.Path() == nil || *b.Path() != snapshot {
						t.Fatal("update did not retain its original server snapshot for restoration")
					}
					assertRestoreTree(t, server, partial)
					return b.restore(destination, restoreOperations{
						copy: func(source, target string) error {
							if source != snapshot {
								t.Fatalf("restoring %q instead of the original snapshot %q", source, snapshot)
							}
							assertRestoreTree(t, source, saved)
							if restoreFails {
								return restoreErr
							}
							return copyDirectory(source, target)
						},
						rename:    os.Rename,
						removeAll: os.RemoveAll,
					})
				},
			}
			removed := false
			err := m.updateBepInEx("fixture", "framework", func(source, target string) error {
				if b.Path() == nil || source != m.modDirectory || filepath.Dir(target) != parent {
					t.Fatal("plugin staging did not follow the original server backup")
				}
				snapshot, plugins = *b.Path(), target
				assertRestoreTree(t, snapshot, saved)
				return copyDirectory(source, target)
			}, os.RemoveAll, func(path string) error {
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
				if path == filepath.Join(server, "BepInEx") {
					removed = true
					assertRestoreTree(t, server, partial)
					return removeErr
				}
				return nil
			})
			for _, expected := range []error{ErrFrameworkUpdateFailed, ErrFrameworkDeleteFailed, removeErr} {
				if !errors.Is(err, expected) {
					t.Fatalf("error omits %v: %v", expected, err)
				}
			}
			if !removed || requests != 0 {
				t.Fatalf("expected partial removal without a download: removed=%t, requests=%d", removed, requests)
			}
			if plugins == "" || !strings.Contains(err.Error(), plugins) {
				t.Fatalf("update error omits the independent plugin snapshot: %v", err)
			}
			assertRestoreTree(t, plugins, map[string]string{"user": "original plugin", "nested/config": "plugin configuration"})
			if restoreFails {
				if !errors.Is(err, ErrBackupRestoreFailed) || !errors.Is(err, restoreErr) {
					t.Fatalf("restoration failure was lost: %v", err)
				}
				if b.Path() == nil || *b.Path() != snapshot || !strings.Contains(err.Error(), snapshot) {
					t.Fatalf("failed restoration lost the original backup or its recovery path: %v", err)
				}
				assertRestoreTree(t, server, partial)
				assertRestoreTree(t, snapshot, saved)
				if err := b.Restore(server); err != nil {
					t.Fatalf("retrying the original backup failed: %v", err)
				}
			} else if errors.Is(err, ErrBackupRestoreFailed) || errors.Is(err, ErrBackupMissing) {
				t.Fatalf("partial removal did not restore successfully: %v", err)
			}
			assertRestoreTree(t, server, saved)
			if b.Path() != nil {
				t.Error("successful restoration did not consume its server snapshot")
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("restoration orphaned server backups: %v, %v", entries, err)
			}
		})
	}
}

func TestUpdateBepInExRecoveryErrors(t *testing.T) {
	for name, cleanupFailure := range map[string]bool{
		"given failed restoration, when a download fails after removal, then report the original retryable snapshot": false,
		"given partial cleanup after restoration, when returning, then report leftovers without a retryable backup":  true,
	} {
		t.Run(name, func(t *testing.T) {
			server := filepath.Join(restoreTempDir(t), "server")
			t.Setenv("TMPDIR", t.TempDir())
			saved := map[string]string{
				"BepInEx/core/runtime":    "original framework",
				"BepInEx/plugins/user":    "original plugin",
				"BepInEx/config/user.cfg": "user configuration",
				"doorstop_config.ini":     "loader configuration",
				"server-data":             "unrelated bytes",
			}
			writeRestoreTree(t, server, saved)
			b := NewBackup().(*backup)
			fault := errors.New("recovery interrupted")
			var snapshot, plugins string
			m := NewManager(&pluginHTTPClient{GetFunc: func(string) (*http.Response, error) {
				if b.Path() == nil || *b.Path() != snapshot {
					t.Fatal("framework removal replaced or consumed the original snapshot")
				}
				assertRestoreTree(t, snapshot, saved)
				assertRestoreTree(t, server, map[string]string{"server-data": "unrelated bytes"})
				return nil, http.ErrContentLength
			}}, server).(*manager)
			m.backup = &frameworkBackup{
				Backup: b, remove: b.Remove,
				restore: func(destination string) error {
					return b.restore(destination, restoreOperations{
						copy: func(source, target string) error {
							if source != snapshot {
								t.Fatalf("restoring %q instead of the original snapshot %q", source, snapshot)
							}
							if !cleanupFailure {
								return fault
							}
							return copyDirectory(source, target)
						},
						rename: os.Rename,
						removeAll: func(path string) error {
							if path == snapshot {
								if err := os.Remove(filepath.Join(path, "BepInEx", "core", "runtime")); err != nil {
									t.Fatal(err)
								}
								return fault
							}
							return os.RemoveAll(path)
						},
					})
				},
			}
			err := m.updateBepInEx("fixture", "framework", func(source, target string) error {
				if b.Path() == nil {
					t.Fatal("plugin staging started without a server snapshot")
				}
				snapshot, plugins = *b.Path(), target
				assertRestoreTree(t, snapshot, saved)
				return copyDirectory(source, target)
			}, os.RemoveAll, os.RemoveAll)
			for _, expected := range []error{ErrFrameworkUpdateFailed, api.ErrHTTPClient, http.ErrContentLength, fault} {
				if !errors.Is(err, expected) {
					t.Errorf("error omits %v: %v", expected, err)
				}
			}
			for _, path := range []string{snapshot, plugins} {
				if path == "" || err == nil || !strings.Contains(err.Error(), path) {
					t.Errorf("error omits recovery path %q: %v", path, err)
				}
			}
			assertRestoreTree(t, plugins, map[string]string{"user": "original plugin"})
			if cleanupFailure {
				if !errors.Is(err, ErrBackupDeleteFailed) || errors.Is(err, ErrBackupRestoreFailed) {
					t.Errorf("successful restoration was not distinguished from cleanup: %v", err)
				}
				if b.Path() != nil {
					t.Fatal("partially deleted snapshot is still active")
				}
				if err := b.Restore(server); !errors.Is(err, ErrBackupMissing) {
					t.Fatalf("consumed backup remained available: %v", err)
				}
				leftovers := make(map[string]string)
				for name, contents := range saved {
					if name != "BepInEx/core/runtime" {
						leftovers[name] = contents
					}
				}
				assertRestoreTree(t, snapshot, leftovers)
			} else {
				if !errors.Is(err, ErrBackupRestoreFailed) || b.Path() == nil || *b.Path() != snapshot {
					t.Fatalf("restoration failure did not retain the original backup: %v", err)
				}
				assertRestoreTree(t, snapshot, saved)
				assertRestoreTree(t, server, map[string]string{"server-data": "unrelated bytes"})
				if err := b.Restore(server); err != nil {
					t.Fatalf("retrying the original backup failed: %v", err)
				}
			}
			assertRestoreTree(t, server, saved)
		})
	}
}

func TestFrameworkFinalizationErrors(t *testing.T) {
	tests := map[string]struct {
		operation     string
		pluginCleanup bool
		expected      error
	}{
		"given an installed replacement, when snapshot cleanup partially fails, then keep the update and consume the handle": {
			operation: "update", expected: ErrFrameworkUpdateFailed,
		},
		"given completed plugin transfer, when local cleanup fails, then finalize the server snapshot without rollback": {
			operation: "update", pluginCleanup: true, expected: ErrFrameworkUpdateFailed,
		},
		"given a standalone installation, when snapshot cleanup partially fails, then report installed files and leftovers": {
			operation: "install", expected: ErrFrameworkInstallFailed,
		},
		"given standalone removal, when snapshot cleanup partially fails, then report removed files and leftovers": {
			operation: "remove", expected: ErrFrameworkDeleteFailed,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := filepath.Join(restoreTempDir(t), "server")
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			saved := map[string]string{"server-data": "unrelated bytes"}
			if tt.operation != "install" {
				saved["BepInEx/core/old"] = "original framework"
				saved["BepInEx/plugins/user"] = "original plugin"
			}
			writeRestoreTree(t, server, saved)
			archive := frameworkReplacementArchive(t)
			requests := 0
			m := NewManager(&pluginHTTPClient{GetFunc: func(string) (*http.Response, error) {
				requests++
				return &http.Response{Body: io.NopCloser(bytes.NewReader(archive))}, nil
			}}, server).(*manager)
			b := NewBackup().(*backup)
			fault := errors.New("snapshot cleanup interrupted")
			var artifact string
			m.backup = &frameworkBackup{
				Backup: b, restore: b.Restore,
				remove: func() error {
					return b.remove(func(path string) error {
						if b.Path() != nil {
							t.Fatal("cleanup began with a still-restorable snapshot")
						}
						if tt.pluginCleanup {
							return os.RemoveAll(path)
						}
						artifact = path
						if err := os.Remove(filepath.Join(path, "server-data")); err != nil {
							t.Fatal(err)
						}
						return fault
					})
				},
			}
			var err error
			want := map[string]string{"server-data": "unrelated bytes"}
			switch tt.operation {
			case "update":
				err = m.updateBepInEx("fixture", "framework", copyDirectory, func(path string) error {
					if tt.pluginCleanup && filepath.Dir(path) == m.modDirectory {
						artifact = path
						return fault
					}
					return os.RemoveAll(path)
				}, os.RemoveAll)
				want["BepInEx/plugins/user"] = "original plugin"
			case "install":
				path, installErr := m.InstallBepInEx("fixture", "framework")
				err = installErr
				if path != "" {
					t.Errorf("cleanup failure returned a successful installation path %q", path)
				}
			case "remove":
				err = m.RemoveBepInEx()
			}
			if tt.operation != "remove" {
				want["BepInEx/core/runtime"] = "replacement framework"
				want["BepInEx/plugins/bundled"] = "bundled plugin"
				if requests != 1 {
					t.Errorf("expected one download, got %d", requests)
				}
			} else if requests != 0 {
				t.Error("removal downloaded a framework")
			}
			if !errors.Is(err, tt.expected) || !errors.Is(err, ErrBackupDeleteFailed) || !errors.Is(err, fault) {
				t.Fatalf("cleanup error was lost: %v", err)
			}
			if artifact == "" || !strings.Contains(err.Error(), artifact) {
				t.Errorf("cleanup error omits leftover path %q: %v", artifact, err)
			}
			if info, err := os.Stat(artifact); err != nil || !info.IsDir() {
				t.Errorf("reported cleanup artifact is unavailable: %v", err)
			}
			if b.Path() != nil {
				t.Fatal("finalized operation retained an active server snapshot")
			}
			if err := b.Restore(server); !errors.Is(err, ErrBackupMissing) {
				t.Errorf("finalized snapshot could still be restored: %v", err)
			}
			assertRestoreTree(t, server, want)
			entries, err := os.ReadDir(backups)
			if err != nil {
				t.Fatal(err)
			}
			if tt.pluginCleanup {
				if len(entries) != 0 {
					t.Errorf("plugin cleanup failure orphaned a server snapshot: %v", entries)
				}
			} else if len(entries) != 1 || entries[0].Name() != filepath.Base(artifact) {
				t.Errorf("cleanup left unreported server backups: %v", entries)
			}
			if !tt.pluginCleanup {
				delete(saved, "server-data")
				assertRestoreTree(t, artifact, saved)
			}
			stages, err := filepath.Glob(filepath.Join(filepath.Dir(server), ".warden-plugins-*"))
			if err != nil || len(stages) != 0 {
				t.Errorf("completed transfer retained an independent plugin snapshot: %v, %v", stages, err)
			}
		})
	}
}

func TestStandaloneFrameworkRollback(t *testing.T) {
	for name, remove := range map[string]bool{
		"given an invalid archive, when installing directly, then restore the original server": false,
		"given partial removal, when removing directly, then restore the original server":      true,
	} {
		t.Run(name, func(t *testing.T) {
			server := filepath.Join(restoreTempDir(t), "server")
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			saved := map[string]string{
				"BepInEx/core/runtime":    "original framework",
				"BepInEx/plugins/user":    "original plugin",
				"BepInEx/config/user.cfg": "user configuration",
				"server-data":             "unrelated bytes",
			}
			writeRestoreTree(t, server, saved)
			m := NewManager(&pluginHTTPClient{GetFunc: func(string) (*http.Response, error) {
				if remove {
					t.Fatal("removal attempted a download")
				}
				return &http.Response{Body: io.NopCloser(strings.NewReader("invalid archive"))}, nil
			}}, server).(*manager)
			var err error
			if remove {
				fault := errors.New("framework removal interrupted")
				err = m.removeBepInEx(func(path string) error {
					if filepath.Base(path) == "doorstop_libs" {
						assertRestoreTree(t, server, map[string]string{"server-data": "unrelated bytes"})
						return fault
					}
					return os.RemoveAll(path)
				})
				if !errors.Is(err, ErrFrameworkDeleteFailed) || !errors.Is(err, fault) {
					t.Fatalf("removal error was lost: %v", err)
				}
			} else {
				path, installErr := m.InstallBepInEx("fixture", "framework")
				err = installErr
				if path != "" || !errors.Is(err, ErrZipReadFailed) {
					t.Fatalf("expected archive failure, got path=%q, error=%v", path, err)
				}
			}
			if errors.Is(err, ErrBackupMissing) || errors.Is(err, ErrBackupRestoreFailed) || m.backup.Path() != nil {
				t.Errorf("standalone rollback failed or retained its snapshot: %v", err)
			}
			assertRestoreTree(t, server, saved)
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("standalone rollback orphaned snapshots: %v, %v", entries, err)
			}
		})
	}
}

func frameworkReplacementArchive(t *testing.T) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, name := range []string{
		"BepInExPack_Valheim/", "BepInExPack_Valheim/BepInEx/",
		"BepInExPack_Valheim/BepInEx/core/", "BepInExPack_Valheim/BepInEx/plugins/",
		"BepInExPack_Valheim/BepInEx/core/runtime", "BepInExPack_Valheim/BepInEx/plugins/bundled",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		contents := ""
		switch filepath.Base(name) {
		case "runtime":
			contents = "replacement framework"
		case "bundled":
			contents = "bundled plugin"
		}
		if _, err := io.WriteString(entry, contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}
