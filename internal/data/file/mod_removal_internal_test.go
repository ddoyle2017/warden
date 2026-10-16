package file

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStageAllModsRemovalPreparationFailures(t *testing.T) {
	for name, failure := range map[string]string{
		"given replacement creation fails, when staging all mods, then leave the live tree untouched":     "mkdir",
		"given replacement permissions fail, when staging all mods, then leave the live tree untouched":   "chmod",
		"given replacement publication fails, when staging all mods, then recover the original live tree": "publish",
	} {
		t.Run(name, func(t *testing.T) {
			server, backups := restoreTempDir(t), t.TempDir()
			t.Setenv("TMPDIR", backups)
			m := NewManager(&pluginHTTPClient{}, server).(*manager)
			plugins := m.modDirectory
			saved := map[string]string{"a/plugin": "original plugin\x00bytes", "other.dll": "other plugin"}
			writeRestoreTree(t, plugins, saved)
			if err := os.Chmod(plugins, 0o775); err != nil {
				t.Fatal(err)
			}
			unrelated := filepath.Join(server, "server-data")
			if err := os.WriteFile(unrelated, []byte("unrelated bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			fault := errors.New("replacement " + failure + " failed")
			var staging, snapshot string
			mkdirCalls, chmodCalls, renameCalls := 0, 0, 0
			removal, err := m.stageModRemovalWithOperations(plugins, true, ErrDeleteAllModsFailed, modRemovalOperations{
				mkdir: func(path string, mode os.FileMode) error {
					mkdirCalls++
					staging = filepath.Dir(path)
					if filepath.Base(path) != "replacement" || filepath.Dir(staging) != filepath.Dir(plugins) || mode.Perm() != 0o775 {
						t.Fatalf("unexpected replacement preparation: %q, mode %v", path, mode)
					}
					assertRestoreTree(t, plugins, saved)
					assertRestoreRootMode(t, plugins, 0o775)
					assertRestoreRootMode(t, staging, 0o700)
					entries, err := os.ReadDir(backups)
					if err != nil || len(entries) != 1 {
						t.Fatalf("expected one complete snapshot: %v, %v", entries, err)
					}
					snapshot = filepath.Join(backups, entries[0].Name())
					assertRestoreTree(t, snapshot, saved)
					assertRestoreRootMode(t, snapshot, 0o700)
					if failure == "mkdir" {
						return fault
					}
					return os.Mkdir(path, mode)
				},
				chmod: func(path string, mode os.FileMode) error {
					chmodCalls++
					if failure == "mkdir" || path != filepath.Join(staging, "replacement") || mode.Perm() != 0o775 {
						t.Fatalf("unexpected replacement chmod: %q, mode %v", path, mode)
					}
					assertRestoreTree(t, plugins, saved)
					assertRestoreRootMode(t, plugins, 0o775)
					if failure == "chmod" {
						return fault
					}
					return os.Chmod(path, mode)
				},
				rename: func(from, to string) error {
					renameCalls++
					if failure != "publish" {
						t.Fatalf("moved live files before preparation succeeded: %s -> %s", from, to)
					}
					if from == plugins && to == filepath.Join(staging, "contents") {
						return os.Rename(from, to)
					}
					if from != filepath.Join(staging, "replacement") || to != plugins {
						t.Fatalf("unexpected publication: %s -> %s", from, to)
					}
					if _, err := os.Lstat(plugins); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("live directory was not staged: %v", err)
					}
					assertRestoreTree(t, filepath.Join(staging, "contents"), saved)
					assertRestoreRootMode(t, filepath.Join(staging, "contents"), 0o775)
					assertRestoreRootMode(t, staging, 0o700)
					assertRestoreRootMode(t, snapshot, 0o700)
					assertRestoreRootMode(t, from, 0o775)
					return fault
				},
			})
			if removal != nil || !errors.Is(err, ErrDirectoryCreateFailed) || !errors.Is(err, fault) {
				t.Fatalf("expected wrapped preparation/publication failure and no removal handle: %v, %v", removal, err)
			}
			wantChmod, wantRenames := 1, 0
			if failure == "mkdir" {
				wantChmod = 0
			}
			if failure == "publish" {
				wantRenames = 2
			}
			if mkdirCalls != 1 || chmodCalls != wantChmod || renameCalls != wantRenames {
				t.Errorf("unexpected preparation calls: mkdir=%d, chmod=%d, rename=%d", mkdirCalls, chmodCalls, renameCalls)
			}
			assertRestoreTree(t, plugins, saved)
			assertRestoreRootMode(t, plugins, 0o775)
			if data, err := os.ReadFile(unrelated); err != nil || string(data) != "unrelated bytes" {
				t.Errorf("staging changed unrelated bytes: %q, %v", data, err)
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("failed staging left a snapshot: %v, %v", entries, err)
			}
			if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("failed staging left temporary files: %v", err)
			}
		})
	}
}
