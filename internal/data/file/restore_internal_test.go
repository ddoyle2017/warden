package file

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func TestRestoreTransferFailures(t *testing.T) {
	tests := map[string]struct {
		copyFailure    bool
		retainFailure  bool
		publishFailure bool
		recoverFailure bool
		cleanupFailure bool
		missing        bool
	}{
		"given a partial copy failure, when restoring, then retain the live tree and retryable backup": {
			copyFailure: true,
		},
		"given a copy and cleanup failure, when restoring, then report both errors and the partial copy": {
			copyFailure: true, cleanupFailure: true,
		},
		"given a failed prior-tree rename, when restoring, then leave live files untouched": {
			retainFailure: true,
		},
		"given a failed publish, when restoring, then recover the prior tree": {
			publishFailure: true,
		},
		"given a missing destination and failed publish, when restoring, then retain the backup for retry": {
			publishFailure: true, missing: true,
		},
		"given failed publishing and recovery, when restoring, then retain both trees and report their paths": {
			publishFailure: true, recoverFailure: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			source, parent := t.TempDir(), restoreTempDir(t)
			t.Setenv("TMPDIR", t.TempDir())
			saved := map[string]string{"a-plugin": "saved plugin", "nested/config": "saved configuration"}
			live := map[string]string{"live-only": "irreplaceable live bytes"}
			writeRestoreTree(t, source, saved)
			destination := filepath.Join(parent, "server")
			if !tt.missing {
				writeRestoreTree(t, destination, live)
			}
			b := NewBackup().(*backup)
			if err := b.Create(source); err != nil {
				t.Fatal(err)
			}
			snapshot := *b.Path()
			copyErr, recoveryErr, cleanupErr := errors.New("copy interrupted"), errors.New("recovery interrupted"), errors.New("cleanup interrupted")
			var staging string
			ops := restoreOperations{
				copy: func(source, target string) error {
					staging = filepath.Dir(target)
					if tt.copyFailure {
						if err := copyFile(filepath.Join(source, "a-plugin"), filepath.Join(target, "a-plugin")); err != nil {
							t.Fatal(err)
						}
						return copyErr
					}
					return copyDirectory(source, target)
				},
				rename: func(source, target string) error {
					if tt.copyFailure {
						t.Fatal("attempted publication after an incomplete copy")
					}
					if (tt.retainFailure && source == destination) || (tt.publishFailure && filepath.Base(source) == "replacement") {
						return &os.LinkError{Op: "rename", Old: source, New: target, Err: syscall.EXDEV}
					}
					if tt.recoverFailure && filepath.Base(source) == "previous" {
						return recoveryErr
					}
					return os.Rename(source, target)
				},
				removeAll: func(path string) error {
					if tt.cleanupFailure && path == staging {
						return cleanupErr
					}
					return os.RemoveAll(path)
				},
			}

			err := b.restore(destination, ops)
			if !errors.Is(err, ErrBackupRestoreFailed) {
				t.Fatalf("expected restoration failure, got %v", err)
			}
			if tt.copyFailure && !errors.Is(err, copyErr) {
				t.Errorf("copy error was lost: %v", err)
			}
			if (tt.retainFailure || tt.publishFailure) && !errors.Is(err, syscall.EXDEV) {
				t.Errorf("rename error was lost: %v", err)
			}
			if tt.cleanupFailure && (!errors.Is(err, cleanupErr) || !errors.Is(err, ErrBackupDeleteFailed) || !strings.Contains(err.Error(), staging)) {
				t.Errorf("cleanup error or recovery path was lost: %v", err)
			}
			if b.Path() == nil || *b.Path() != snapshot {
				t.Fatal("failed restoration discarded the backup")
			}
			assertRestoreTree(t, snapshot, saved)
			if tt.missing || tt.recoverFailure {
				if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("expected destination to remain absent, got %v", err)
				}
			} else {
				assertRestoreTree(t, destination, live)
			}
			if tt.recoverFailure {
				previous, replacement := filepath.Join(staging, "previous"), filepath.Join(staging, "replacement")
				if !errors.Is(err, recoveryErr) {
					t.Errorf("recovery error was lost: %v", err)
				}
				for _, path := range []string{destination, previous, replacement, snapshot} {
					if !strings.Contains(err.Error(), path) {
						t.Errorf("error omits recovery path %q: %v", path, err)
					}
				}
				assertRestoreTree(t, previous, live)
				assertRestoreTree(t, replacement, saved)
				if err := os.Rename(previous, destination); err != nil {
					t.Fatalf("manual recovery from the reported path failed: %v", err)
				}
				assertRestoreTree(t, destination, live)
			}
			if tt.cleanupFailure {
				assertRestoreTree(t, filepath.Join(staging, "replacement"), map[string]string{"a-plugin": "saved plugin"})
			}
			if !tt.cleanupFailure && !tt.recoverFailure {
				if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("temporary copy was not removed: %v", err)
				}
			}
			if err := b.Restore(destination); err != nil {
				t.Fatalf("retry failed: %v", err)
			}
			assertRestoreTree(t, destination, saved)
			if b.Path() != nil {
				t.Error("successful retry did not consume the backup")
			}
		})
	}
}

func TestRestoreCrossDeviceTransfer(t *testing.T) {
	source, parent := t.TempDir(), restoreTempDir(t)
	t.Setenv("TMPDIR", t.TempDir())
	saved := map[string]string{"plugin": "backup bytes"}
	writeRestoreTree(t, source, saved)
	destination := filepath.Join(parent, "server")
	writeRestoreTree(t, destination, map[string]string{"old": "live bytes"})
	b := NewBackup().(*backup)
	if err := b.Create(source); err != nil {
		t.Fatal(err)
	}
	snapshot, renamed := *b.Path(), false
	err := b.restore(destination, restoreOperations{
		copy: copyDirectory,
		rename: func(source, target string) error {
			// Treat the backup directory as another device, without mounts.
			if strings.HasPrefix(source, snapshot+string(os.PathSeparator)) || source == snapshot {
				return &os.LinkError{Op: "rename", Old: source, New: target, Err: syscall.EXDEV}
			}
			for _, path := range []string{source, target} {
				if !strings.HasPrefix(path, parent+string(os.PathSeparator)) {
					t.Fatalf("rename is not destination-local: %s -> %s", source, target)
				}
			}
			renamed = true
			return os.Rename(source, target)
		},
		removeAll: os.RemoveAll,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !renamed {
		t.Fatal("restoration bypassed the injected rename operation")
	}
	assertRestoreTree(t, destination, saved)
	if b.Path() != nil {
		t.Error("successful restoration retained the active backup")
	}
}

func TestRestoreCleanupFailures(t *testing.T) {
	tests := map[string]struct{ snapshot, previous bool }{
		"given partial snapshot cleanup, when restoration succeeds, then report leftovers without offering an incomplete backup": {snapshot: true},
		"given prior-tree cleanup failure, when restoration succeeds, then retain restored files and report the old tree":        {previous: true},
		"given both cleanup failures, when restoration succeeds, then report both errors and paths":                              {snapshot: true, previous: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			source, parent := t.TempDir(), restoreTempDir(t)
			t.Setenv("TMPDIR", t.TempDir())
			saved := map[string]string{"a-plugin": "saved plugin", "z-config": "saved config"}
			live := map[string]string{"old": "live bytes"}
			writeRestoreTree(t, source, saved)
			destination := filepath.Join(parent, "server")
			writeRestoreTree(t, destination, live)
			b := NewBackup().(*backup)
			if err := b.Create(source); err != nil {
				t.Fatal(err)
			}
			snapshot := *b.Path()
			snapshotErr, previousErr := errors.New("snapshot cleanup failed"), errors.New("prior-tree cleanup failed")
			var staging string
			err := b.restore(destination, restoreOperations{
				copy:   copyDirectory,
				rename: os.Rename,
				removeAll: func(path string) error {
					if path == snapshot && tt.snapshot {
						if err := os.Remove(filepath.Join(snapshot, "a-plugin")); err != nil {
							t.Fatal(err)
						}
						return snapshotErr
					}
					if path != snapshot && tt.previous {
						staging = path
						return previousErr
					}
					return os.RemoveAll(path)
				},
			})
			if !errors.Is(err, ErrBackupDeleteFailed) || errors.Is(err, ErrBackupRestoreFailed) {
				t.Fatalf("expected only a cleanup failure after publication, got %v", err)
			}
			assertRestoreTree(t, destination, saved)
			if tt.snapshot {
				if !errors.Is(err, snapshotErr) || !strings.Contains(err.Error(), snapshot) {
					t.Errorf("snapshot cleanup details were lost: %v", err)
				}
				assertRestoreTree(t, snapshot, map[string]string{"z-config": "saved config"})
			}
			if tt.previous {
				if !errors.Is(err, previousErr) || !strings.Contains(err.Error(), staging) {
					t.Errorf("prior-tree cleanup details were lost: %v", err)
				}
				assertRestoreTree(t, filepath.Join(staging, "previous"), live)
			}
			if b.Path() != nil {
				t.Fatal("a consumed, possibly incomplete snapshot is still active")
			}
			if err := b.Restore(destination); !errors.Is(err, ErrBackupMissing) {
				t.Errorf("consumed snapshot was available for retry: %v", err)
			}
			assertRestoreTree(t, destination, saved)
		})
	}
}

func TestRestoreUnsafeRoots(t *testing.T) {
	source := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{"", ".", "..", string(os.PathSeparator), cwd, filepath.Dir(cwd)} {
		t.Run("given an unsafe root "+destination+", when validating, then reject it before filesystem mutation", func(t *testing.T) {
			if _, err := restoreDestination(source, destination); err == nil {
				t.Error("unsafe restoration target was accepted")
			}
		})
	}
}

func writeRestoreTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func restoreTempDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func assertRestoreTree(t *testing.T, root string, want map[string]string) {
	t.Helper()
	got := make(map[string]string)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got[relative] = string(contents)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tree at %q = %v, want %v", root, got, want)
	}
}
