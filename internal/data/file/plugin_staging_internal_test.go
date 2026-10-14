package file

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/test/mock"
)

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
			m := NewManager(&mock.HTTPClient{GetFunc: func(string) (*http.Response, error) {
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
			})
			if !errors.Is(err, ErrFrameworkUpdateFailed) || !errors.Is(err, copyErr) {
				t.Fatalf("expected staging failure, got %v", err)
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
