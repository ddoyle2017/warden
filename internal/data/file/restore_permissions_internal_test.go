package file

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreReadOnlyRoots(t *testing.T) {
	for _, mode := range []os.FileMode{0o500, 0o555} {
		t.Run(fmt.Sprintf("given a %04o saved root, when restoring a missing destination, then preserve its bytes and mode", mode), func(t *testing.T) {
			root, backups := restorePermissionsTempDir(t), t.TempDir()
			t.Setenv("TMPDIR", backups)
			source, destination := filepath.Join(root, "source"), filepath.Join(root, "restored")
			saved := map[string]string{"plugin": "saved plugin\x00bytes", "nested/config": "saved settings"}
			writeRestoreTree(t, source, saved)
			if err := os.Chmod(filepath.Join(source, "plugin"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(source, mode); err != nil {
				t.Fatal(err)
			}
			b := NewBackup()
			if err := b.Create(source); err != nil {
				t.Fatal(err)
			}
			assertRestoreRootMode(t, *b.Path(), 0o700)

			if err := b.Restore(destination); err != nil {
				t.Fatal(err)
			}
			assertRestoreTree(t, destination, saved)
			assertRestoreRootMode(t, destination, mode)
			assertRestoreTree(t, source, saved)
			assertRestoreRootMode(t, source, mode)
			if info, err := os.Stat(filepath.Join(destination, "plugin")); err != nil || info.Mode().Perm() != 0o644 {
				t.Errorf("restored file permissions changed: %v, %v", info, err)
			}
			if b.Path() != nil {
				t.Error("successful restoration left an active backup")
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("successful restoration left backup files: %v, %v", entries, err)
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 2 {
				t.Errorf("successful restoration left staging: %v, %v", entries, err)
			}
		})
	}
}

func TestRestoreReadOnlyRetainFailure(t *testing.T) {
	root := restorePermissionsTempDir(t)
	t.Setenv("TMPDIR", t.TempDir())
	source, destination := filepath.Join(root, "source"), filepath.Join(root, "live")
	saved, live := map[string]string{"saved": "snapshot bytes"}, map[string]string{"live": "original live bytes"}
	writeRestoreTree(t, source, saved)
	writeRestoreTree(t, destination, live)
	for _, path := range []string{source, destination} {
		if err := os.Chmod(path, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	b := NewBackup().(*backup)
	if err := b.Create(source); err != nil {
		t.Fatal(err)
	}
	snapshot := *b.Path()
	retainErr := errors.New("prior-tree rename failed")
	err := b.restore(destination, restoreOperations{
		copy: copyDirectory, chmod: os.Chmod, removeAll: os.RemoveAll,
		rename: func(from, to string) error {
			if from != destination || filepath.Base(to) != "previous" {
				t.Fatalf("unexpected rename after retain failure: %s -> %s", from, to)
			}
			assertRestoreRootMode(t, filepath.Dir(to), 0o700)
			return retainErr
		},
	})
	if !errors.Is(err, ErrBackupRestoreFailed) || !errors.Is(err, retainErr) || errors.Is(err, ErrBackupDeleteFailed) {
		t.Fatalf("expected only the injected retain failure, got %v", err)
	}
	assertRestoreTree(t, destination, live)
	assertRestoreRootMode(t, destination, 0o555)
	if b.Path() == nil || *b.Path() != snapshot {
		t.Fatal("failed restoration discarded the backup")
	}
	assertRestoreTree(t, snapshot, saved)
	assertRestoreRootMode(t, snapshot, 0o700)
	if stages, err := filepath.Glob(filepath.Join(root, ".warden-restore-*")); err != nil || len(stages) != 0 {
		t.Errorf("failed restoration left a prepared copy: %v, %v", stages, err)
	}
	recovered := filepath.Join(root, "recovered")
	if err := b.Restore(recovered); err != nil {
		t.Fatalf("retained backup was not usable: %v", err)
	}
	assertRestoreTree(t, recovered, saved)
	assertRestoreRootMode(t, recovered, 0o555)
	assertRestoreTree(t, destination, live)
	assertRestoreRootMode(t, destination, 0o555)
}

func TestRestoreFinalModeFailures(t *testing.T) {
	tests := map[string]struct {
		existing        bool
		withdrawFailure bool
		recoverFailure  bool
	}{
		"given a missing destination, when final permissions fail, then withdraw the copy and retain the backup": {},
		"given an existing destination, when final permissions fail, then recover its original tree":             {existing: true},
		"given a missing destination, when final permissions and withdrawal fail, then retain the published copy and backup": {
			withdrawFailure: true,
		},
		"given an existing destination, when final permissions and withdrawal fail, then retain both trees and backup": {
			existing: true, withdrawFailure: true,
		},
		"given an existing destination, when final permissions and prior-tree recovery fail, then retain both staged trees": {
			existing: true, recoverFailure: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			root := restorePermissionsTempDir(t)
			t.Setenv("TMPDIR", t.TempDir())
			source, destination := filepath.Join(root, "source"), filepath.Join(root, "live")
			saved := map[string]string{"saved": "snapshot bytes", "nested/config": "saved settings"}
			writeRestoreTree(t, source, saved)
			if err := os.Chmod(source, 0o500); err != nil {
				t.Fatal(err)
			}
			if tt.existing {
				writeRestoreTree(t, destination, map[string]string{"original": "live bytes"})
				// Owner write permits real Linux renames; missing read access requires finalization.
				if err := os.Chmod(destination, 0o300); err != nil {
					t.Fatal(err)
				}
			}
			b := NewBackup().(*backup)
			if err := b.Create(source); err != nil {
				t.Fatal(err)
			}
			snapshot := *b.Path()
			modeErr, withdrawErr, recoverErr := errors.New("final chmod failed"), errors.New("withdrawal failed"), errors.New("prior-tree recovery failed")
			var staging string
			finalizations := 0
			err := b.restore(destination, restoreOperations{
				copy: func(from, to string) error {
					staging = filepath.Dir(to)
					if from != snapshot {
						t.Fatalf("copied an unexpected snapshot: %s", from)
					}
					return copyDirectory(from, to)
				},
				chmod: func(path string, mode os.FileMode) error {
					assertRestoreRootMode(t, snapshot, 0o700)
					assertRestoreRootMode(t, staging, 0o700)
					if path == destination {
						finalizations++
						wantMode := os.FileMode(0o500)
						if tt.existing {
							wantMode = 0o300
						}
						if mode.Perm() != wantMode {
							t.Errorf("final mode = %04o, want %04o", mode.Perm(), wantMode)
						}
						assertRestoreTree(t, destination, saved)
						assertRestoreRootMode(t, destination, 0o700)
						if b.Path() == nil || *b.Path() != snapshot {
							t.Fatal("backup was consumed before final permissions succeeded")
						}
						return modeErr
					}
					if path != filepath.Join(staging, "replacement") {
						t.Fatalf("chmod touched an unexpected path: %s", path)
					}
					return os.Chmod(path, mode)
				},
				rename: func(from, to string) error {
					previous, replacement := filepath.Join(staging, "previous"), filepath.Join(staging, "replacement")
					switch {
					case tt.existing && from == destination && to == previous:
					case from == replacement && to == destination:
					case from == destination && to == replacement:
						if tt.withdrawFailure {
							return withdrawErr
						}
					case tt.existing && from == previous && to == destination:
						if tt.recoverFailure {
							return recoverErr
						}
					default:
						t.Fatalf("unexpected recovery rename: %s -> %s", from, to)
					}
					return os.Rename(from, to)
				},
				removeAll: func(path string) error {
					if path != staging || tt.withdrawFailure || tt.recoverFailure {
						t.Fatalf("attempted to discard recovery data at %q", path)
					}
					return os.RemoveAll(path)
				},
			})
			if !errors.Is(err, ErrBackupRestoreFailed) || !errors.Is(err, modeErr) || finalizations != 1 {
				t.Fatalf("final permission failure was lost: %v; finalizations=%d", err, finalizations)
			}
			if b.Path() == nil || *b.Path() != snapshot {
				t.Fatal("failed finalization consumed the backup")
			}
			assertRestoreTree(t, snapshot, saved)
			assertRestoreRootMode(t, snapshot, 0o700)
			previous, replacement := filepath.Join(staging, "previous"), filepath.Join(staging, "replacement")
			if tt.withdrawFailure || tt.recoverFailure {
				cause := recoverErr
				if tt.withdrawFailure {
					cause = withdrawErr
				}
				if !errors.Is(err, cause) {
					t.Errorf("recovery cause was lost: %v", err)
				}
				for _, path := range []string{destination, staging, snapshot, replacement} {
					if !strings.Contains(err.Error(), path) {
						t.Errorf("error omits recovery path %q: %v", path, err)
					}
				}
				assertRestoreRootMode(t, staging, 0o700)
				if tt.existing {
					assertOriginalRestoreTree(t, previous)
					if !strings.Contains(err.Error(), previous) {
						t.Errorf("error omits the prior tree: %v", err)
					}
				}
				if tt.withdrawFailure {
					assertRestoreTree(t, destination, saved)
					assertRestoreRootMode(t, destination, 0o700)
					if err := os.Rename(destination, replacement); err != nil {
						t.Fatal(err)
					}
				} else if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("failed recovery unexpectedly recreated the destination: %v", err)
				}
				assertRestoreTree(t, replacement, saved)
				if tt.existing {
					if err := os.Rename(previous, destination); err != nil {
						t.Fatalf("manual recovery from the reported path failed: %v", err)
					}
				}
			} else if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("withdrawn replacement was not cleaned up: %v", err)
			}
			if tt.existing {
				assertOriginalRestoreTree(t, destination)
			} else if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("withdrawal did not restore the absent destination: %v", err)
			}
			retry := filepath.Join(root, "retry")
			if err := b.Restore(retry); err != nil {
				t.Fatalf("retained backup could not restore a new target: %v", err)
			}
			assertRestoreTree(t, retry, saved)
			assertRestoreRootMode(t, retry, 0o500)
			if b.Path() != nil {
				t.Error("successful retry did not consume the backup")
			}
		})
	}
}

func assertOriginalRestoreTree(t *testing.T, root string) {
	t.Helper()
	assertRestoreRootMode(t, root, 0o300)
	if data, err := os.ReadFile(filepath.Join(root, "original")); err != nil || string(data) != "live bytes" {
		t.Errorf("original tree bytes changed: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "saved")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("replacement files entered the original tree: %v", err)
	}
}

func assertRestoreRootMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != mode {
		t.Errorf("directory %q = %v, %v; want mode %04o", path, info, err, mode)
	}
}

func restorePermissionsTempDir(t *testing.T) string {
	t.Helper()
	root := restoreTempDir(t)
	t.Cleanup(func() {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return os.Chmod(path, 0o700)
			}
			return nil
		})
		if err != nil {
			t.Errorf("restore access for owned fixture cleanup: %v", err)
		}
	})
	return root
}
