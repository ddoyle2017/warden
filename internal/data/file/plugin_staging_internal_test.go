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
)

func TestCopyDirectoryModes(t *testing.T) {
	source, snapshot, local := t.TempDir(), t.TempDir(), t.TempDir()
	files := map[string]string{"read-only/nested/plugin": "plugin bytes"}
	modes := map[string]os.FileMode{"read-only": 0o555, "read-only/nested": 0o555, "empty": 0o555}
	t.Cleanup(func() {
		for _, root := range []string{source, snapshot, local} {
			for _, name := range []string{"read-only", "read-only/nested", "empty"} {
				if err := os.Chmod(filepath.Join(root, name), 0o700); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Errorf("restore cleanup access at %q: %v", root, err)
				}
			}
		}
	})
	writeRestoreTree(t, source, files)
	if err := os.Mkdir(filepath.Join(source, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0o775); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{snapshot, local} {
		if err := os.Chmod(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, mode := range modes {
		if err := os.Chmod(filepath.Join(source, name), mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, paths := range [][2]string{{source, snapshot}, {snapshot, local}} {
		if err := copyDirectory(paths[0], paths[1]); err != nil {
			t.Fatal(err)
		}
		assertRestoreTree(t, paths[1], files)
		if info, err := os.Stat(paths[1]); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("copy changed private container permissions: %v, %v", info, err)
		}
		for name, mode := range modes {
			info, err := os.Stat(filepath.Join(paths[1], name))
			if err != nil || !info.IsDir() || info.Mode().Perm() != mode {
				t.Errorf("copied directory %q = %v, %v; want mode %04o", name, info, err, mode)
			}
		}
	}
}

func TestUpdateBepInExCleanupFailures(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, entry := range []struct{ name, contents string }{
		{"BepInExPack_Valheim/", ""},
		{"BepInExPack_Valheim/BepInEx/", ""},
		{"BepInExPack_Valheim/BepInEx/core/", ""},
		{"BepInExPack_Valheim/BepInEx/plugins/", ""},
		{"BepInExPack_Valheim/BepInEx/core/runtime", "new framework"},
		{"BepInExPack_Valheim/BepInEx/plugins/bundled", "bundled plugin"},
	} {
		dst, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(dst, entry.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	for name, tt := range map[string]struct{ local, snapshot, partialSnapshot bool }{
		"given local cleanup fails, when publication completes, then retain installed files without rollback": {local: true},
		"given snapshot cleanup fails, when publication completes, then retain its complete recovery copy":    {snapshot: true},
		"given both cleanups fail, when publication completes, then report both causes and retained paths":    {local: true, snapshot: true},
		"given partial snapshot cleanup, when publication completes, then retain installed files and report remaining artifacts": {
			snapshot: true, partialSnapshot: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := filepath.Join(restoreTempDir(t), "server")
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			plugins := map[string]string{"a-plugin": "saved plugin", "nested/config": "saved configuration"}
			writeRestoreTree(t, server, map[string]string{
				"BepInEx/plugins/a-plugin":      plugins["a-plugin"],
				"BepInEx/plugins/nested/config": plugins["nested/config"],
				"BepInEx/config/user.cfg":       "user framework configuration",
				"BepInEx/core/old":              "old framework",
				"server-data":                   "unrelated server bytes",
			})
			requests := 0
			m := NewManager(&pluginHTTPClient{GetFunc: func(url string) (*http.Response, error) {
				requests++
				if url != "fixture-url" {
					t.Fatalf("unexpected download URL %q", url)
				}
				return &http.Response{Body: io.NopCloser(bytes.NewReader(archive.Bytes()))}, nil
			}}, server).(*manager)
			backup := &restoreTrackingBackup{Backup: m.backup}
			m.backup = backup
			localErr, snapshotErr := errors.New("local cleanup interrupted"), errors.New("snapshot cleanup interrupted")
			var snapshot, local string
			err := m.updateBepInEx("fixture-url", "fixture-framework", func(source, target string) error {
				switch source {
				case m.modDirectory:
					snapshot = target
				case snapshot:
					local = target
				default:
					t.Fatalf("unexpected plugin copy source %q", source)
				}
				return copyDirectory(source, target)
			}, func(path string) error {
				switch {
				case path == local:
					if tt.local {
						return localErr
					}
				case path == snapshot:
					if tt.snapshot {
						if tt.partialSnapshot {
							if err := os.Remove(filepath.Join(path, "a-plugin")); err != nil {
								t.Fatal(err)
							}
						}
						return snapshotErr
					}
				case filepath.Dir(path) == filepath.Join(server, "BepInEx", "config") &&
					strings.HasPrefix(filepath.Base(path), ".warden-config-"):
					if data, err := os.ReadFile(filepath.Join(server, "BepInEx", "config", "user.cfg")); err != nil || string(data) != "user framework configuration" {
						t.Fatalf("configuration was not published before cleanup: %q, %v", data, err)
					}
				default:
					t.Fatalf("unexpected cleanup path %q", path)
				}
				return os.RemoveAll(path)
			}, os.RemoveAll)
			if !errors.Is(err, ErrBackupDeleteFailed) ||
				errors.Is(err, localErr) != tt.local || errors.Is(err, snapshotErr) != tt.snapshot {
				t.Fatalf("cleanup errors did not retain their causes: %v", err)
			}
			if _, completed := err.(*FrameworkMutationCleanupError); !completed {
				t.Fatalf("completed update must return a direct cleanup error: %T: %v", err, err)
			}
			if requests != 1 || backup.restores != 0 {
				t.Errorf("completed publication retried or rolled back: downloads=%d, restores=%d", requests, backup.restores)
			}
			if filepath.Dir(snapshot) != filepath.Dir(server) || filepath.Dir(local) != m.modDirectory {
				t.Fatalf("unexpected staging paths: snapshot=%q local=%q", snapshot, local)
			}
			for path, retained := range map[string]bool{local: tt.local, snapshot: tt.snapshot} {
				info, statErr := os.Stat(path)
				if retained {
					if statErr != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || !strings.Contains(err.Error(), path) {
						t.Errorf("retained artifact is not private or reported: %q, %v, %v; error=%v", path, info, statErr, err)
					}
				} else if !errors.Is(statErr, os.ErrNotExist) {
					t.Errorf("successful cleanup retained %q: %v", path, statErr)
				}
			}
			assertRestoreTree(t, server, map[string]string{
				"BepInEx/plugins/a-plugin":      plugins["a-plugin"],
				"BepInEx/plugins/nested/config": plugins["nested/config"],
				"BepInEx/plugins/bundled":       "bundled plugin",
				"BepInEx/config/user.cfg":       "user framework configuration",
				"BepInEx/core/runtime":          "new framework",
				"server-data":                   "unrelated server bytes",
			})
			if tt.local {
				assertRestoreTree(t, local, map[string]string{})
			}
			if tt.snapshot {
				leftovers := plugins
				if tt.partialSnapshot {
					leftovers = map[string]string{"nested/config": plugins["nested/config"]}
				}
				assertRestoreTree(t, snapshot, leftovers)
			}
			if backup.Path() != nil {
				t.Error("completed update retained an active server backup")
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("completed update orphaned server backups: %v, %v", entries, err)
			}
		})
	}
}

type restoreTrackingBackup struct {
	Backup
	restores int
}

func (b *restoreTrackingBackup) Restore(destination string) error {
	b.restores++
	return b.Backup.Restore(destination)
}

type pluginHTTPClient struct {
	GetFunc func(string) (*http.Response, error)
}

func (c *pluginHTTPClient) Get(url string) (*http.Response, error) {
	return c.GetFunc(url)
}

func TestUpdatePluginPreparationFailure(t *testing.T) {
	for name, tt := range map[string]struct{ partial, cleanupFailure bool }{
		"given an initial staging failure, when updating, then retain server files and the backup":                 {},
		"given a partial staging failure, when updating, then remove the partial copy without changing live files": {partial: true},
		"given staging and cleanup failures, when updating, then report both errors and retain live files":         {partial: true, cleanupFailure: true},
	} {
		t.Run(name, func(t *testing.T) {
			parent := restoreTempDir(t)
			server := filepath.Join(parent, "server")
			t.Setenv("TMPDIR", t.TempDir())
			files := map[string]string{
				"BepInEx/plugins/a-plugin": "first plugin",
				"BepInEx/plugins/z-plugin": "last plugin",
				"BepInEx/core/runtime":     "old framework",
				"server-data":              "unrelated server bytes",
			}
			writeRestoreTree(t, server, files)
			requests := 0
			m := NewManager(&pluginHTTPClient{GetFunc: func(string) (*http.Response, error) {
				requests++
				return nil, errors.New("unexpected download")
			}}, server).(*manager)
			copyErr, cleanupErr := errors.New("plugin copy interrupted"), errors.New("staging cleanup interrupted")
			var staging string
			err := m.updateBepInEx("unused", "unused", func(source, target string) error {
				staging = target
				if source != m.modDirectory || filepath.Dir(target) != parent {
					t.Fatalf("unexpected plugin staging paths: %s -> %s", source, target)
				}
				if tt.partial {
					if err := copyFile(filepath.Join(source, "a-plugin"), filepath.Join(target, "a-plugin")); err != nil {
						t.Fatal(err)
					}
				}
				return copyErr
			}, func(path string) error {
				if tt.cleanupFailure {
					return cleanupErr
				}
				return os.RemoveAll(path)
			}, os.RemoveAll)
			if !errors.Is(err, ErrFrameworkUpdateFailed) || !errors.Is(err, copyErr) {
				t.Fatalf("expected staging failure, got %v", err)
			}
			var completed *FrameworkMutationCleanupError
			if errors.As(err, &completed) {
				t.Errorf("failed preparation was reported as a completed update: %v", err)
			}
			if requests != 0 {
				t.Error("framework installation ran after staging failed")
			}
			assertRestoreTree(t, server, files)
			if m.backup.Path() == nil {
				t.Fatal("staging failure discarded the complete server backup")
			}
			assertRestoreTree(t, *m.backup.Path(), files)
			if !strings.Contains(err.Error(), *m.backup.Path()) {
				t.Errorf("staging error omits the retained backup path: %v", err)
			}
			if tt.cleanupFailure {
				if !errors.Is(err, ErrBackupDeleteFailed) || !errors.Is(err, cleanupErr) || !strings.Contains(err.Error(), staging) {
					t.Errorf("cleanup failure omitted its cause or path: %v", err)
				}
				assertRestoreTree(t, staging, map[string]string{"a-plugin": "first plugin"})
			} else if entries, err := os.ReadDir(parent); err != nil || len(entries) != 1 || entries[0].Name() != "server" {
				t.Errorf("partial plugin staging was not cleaned up: %v, %v", entries, err)
			}
		})
	}
}

func TestPluginTransferDestinationFailure(t *testing.T) {
	snapshot, parent := t.TempDir(), t.TempDir()
	saved := map[string]string{"plugin": "saved plugin"}
	writeRestoreTree(t, snapshot, saved)
	blocker := filepath.Join(parent, "blocked")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	transferred, err := copyStagedPlugins(snapshot, filepath.Join(blocker, "plugins"),
		func(string, string) error {
			t.Fatal("plugin copying ran before its destination was prepared")
			return nil
		},
		func(string) error {
			t.Fatal("cleanup ran without transfer staging")
			return nil
		})
	var cause *os.PathError
	if transferred || !errors.Is(err, ErrDirectoryCreateFailed) || !errors.As(err, &cause) {
		t.Fatalf("expected a cause-preserving destination failure, got transferred=%t, error=%v", transferred, err)
	}
	assertRestoreTree(t, snapshot, saved)
	assertRestoreTree(t, parent, map[string]string{"blocked": "not a directory"})
}

func TestPluginTransferPreparation(t *testing.T) {
	for name, tt := range map[string]struct{ copyFailure, cleanupFailure bool }{
		"given a staged snapshot, when transferring plugins, then copy locally and preserve bundled files":          {},
		"given a partial local copy failure, when transferring plugins, then preserve the snapshot and destination": {copyFailure: true},
		"given successful transfer and failed cleanup, when returning, then report a completed transfer and leftover path": {
			cleanupFailure: true,
		},
		"given copy and cleanup failures, when transferring plugins, then retain the snapshot and report the partial copy": {
			copyFailure: true, cleanupFailure: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			snapshot, destination := t.TempDir(), t.TempDir()
			saved := map[string]string{"a-plugin": "plugin bytes", "nested/config": "configuration bytes"}
			bundled := map[string]string{"bundled": "new framework plugin"}
			writeRestoreTree(t, snapshot, saved)
			writeRestoreTree(t, destination, bundled)
			copyErr, cleanupErr := errors.New("local copy interrupted"), errors.New("local cleanup interrupted")
			var local string
			transferred, err := copyStagedPlugins(snapshot, destination, func(source, target string) error {
				local = target
				if source != snapshot || filepath.Dir(target) != destination {
					t.Fatalf("plugin transfer was not staged inside its destination: %s -> %s", source, target)
				}
				if tt.copyFailure {
					if err := copyFile(filepath.Join(source, "a-plugin"), filepath.Join(target, "a-plugin")); err != nil {
						t.Fatal(err)
					}
					return copyErr
				}
				return copyDirectory(source, target)
			}, func(path string) error {
				if tt.cleanupFailure {
					return cleanupErr
				}
				return os.RemoveAll(path)
			})
			if tt.copyFailure {
				if !errors.Is(err, copyErr) || transferred {
					t.Fatalf("expected copy failure, got %v", err)
				}
			} else {
				if (!tt.cleanupFailure && err != nil) || !transferred {
					t.Fatalf("expected a complete transfer, got transferred=%t, error=%v", transferred, err)
				}
				for name, contents := range saved {
					bundled[name] = contents
				}
			}
			if tt.cleanupFailure {
				if !errors.Is(err, ErrBackupDeleteFailed) || !errors.Is(err, cleanupErr) || !strings.Contains(err.Error(), local) {
					t.Errorf("cleanup error or leftover path was lost: %v", err)
				}
				if tt.copyFailure {
					bundled[filepath.Join(filepath.Base(local), "a-plugin")] = "plugin bytes"
				}
			}
			assertRestoreTree(t, snapshot, saved)
			assertRestoreTree(t, destination, bundled)
			entries, err := os.ReadDir(destination)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".warden-plugins-") && !tt.cleanupFailure {
					t.Errorf("local transfer copy was not removed: %s", entry.Name())
				}
			}
			if tt.cleanupFailure {
				if info, err := os.Stat(local); err != nil || !info.IsDir() {
					t.Errorf("reported cleanup artifact is unavailable: %v", err)
				}
			}
		})
	}
}
