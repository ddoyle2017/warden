package file

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreLegacyOperations(t *testing.T) {
	tests := map[string]struct {
		sourceMode        os.FileMode
		destinationMode   os.FileMode
		permissionFailure bool
	}{
		"given legacy overrides and a private destination, when restoring, then preserve the destination mode": {
			sourceMode: 0o775, destinationMode: 0o700,
		},
		"given legacy overrides and a group-writable snapshot, when restoring a missing root, then preserve its mode": {
			sourceMode: 0o775,
		},
		"given legacy overrides and a read-only snapshot, when restoring a missing root, then finalize its mode": {
			sourceMode: 0o555,
		},
		"given legacy overrides and a missing prepared root, when chmod fails, then retain live files and a retryable backup": {
			sourceMode: 0o775, destinationMode: 0o700, permissionFailure: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			root, backups := restorePermissionsTempDir(t), t.TempDir()
			t.Setenv("TMPDIR", backups)
			source, destination := filepath.Join(root, "source"), filepath.Join(root, "destination")
			saved := map[string]string{"payload": "snapshot\x00bytes", "nested/config": "saved settings"}
			live := map[string]string{"live-only": "original live bytes"}
			writeRestoreTree(t, source, saved)
			if err := os.Chmod(filepath.Join(source, "payload"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(source, tt.sourceMode); err != nil {
				t.Fatal(err)
			}
			if tt.destinationMode != 0 {
				writeRestoreTree(t, destination, live)
				if err := os.Chmod(destination, tt.destinationMode); err != nil {
					t.Fatal(err)
				}
			}
			b := NewBackup().(*backup)
			if err := b.Create(source); err != nil {
				t.Fatal(err)
			}
			snapshot := *b.Path()
			var staging string
			err := b.restore(destination, restoreOperations{
				copy: func(from, to string) error {
					staging = filepath.Dir(to)
					if from != snapshot {
						t.Fatalf("unexpected source snapshot: %s", from)
					}
					if tt.permissionFailure {
						return os.Remove(to)
					}
					return copyDirectory(from, to)
				},
				rename: func(from, to string) error {
					if tt.permissionFailure {
						t.Fatal("attempted publication instead of returning the real chmod error")
					}
					assertRestoreRootMode(t, snapshot, 0o700)
					assertRestoreRootMode(t, staging, 0o700)
					return os.Rename(from, to)
				},
				removeAll: os.RemoveAll,
			})
			if tt.permissionFailure {
				var pathErr *os.PathError
				if !errors.Is(err, ErrBackupRestoreFailed) || !errors.Is(err, os.ErrNotExist) ||
					!errors.As(err, &pathErr) || pathErr.Op != "chmod" {
					t.Fatalf("expected the real permission error, got %v", err)
				}
				assertRestoreTree(t, destination, live)
				assertRestoreRootMode(t, destination, tt.destinationMode)
				if b.Path() == nil || *b.Path() != snapshot {
					t.Fatal("permission failure discarded the active backup")
				}
				assertRestoreTree(t, snapshot, saved)
				assertRestoreRootMode(t, snapshot, 0o700)
				if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("failed preparation left staging: %v", err)
				}
				if err := b.restore(destination, restoreOperations{copy: copyDirectory, rename: os.Rename, removeAll: os.RemoveAll}); err != nil {
					t.Fatalf("legacy override retry failed: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			wantMode := tt.sourceMode
			if tt.destinationMode != 0 {
				wantMode = tt.destinationMode
			}
			assertRestoreTree(t, destination, saved)
			assertRestoreRootMode(t, destination, wantMode)
			assertRestoreTree(t, source, saved)
			assertRestoreRootMode(t, source, tt.sourceMode)
			if info, err := os.Stat(filepath.Join(destination, "payload")); err != nil || info.Mode().Perm() != 0o644 {
				t.Errorf("legacy override changed file permissions: %v, %v", info, err)
			}
			if b.Path() != nil {
				t.Error("successful restoration did not consume the backup")
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("successful restoration left backup files: %v, %v", entries, err)
			}
			if stages, err := filepath.Glob(filepath.Join(root, ".warden-restore-*")); err != nil || len(stages) != 0 {
				t.Errorf("legacy override left staging: %v, %v", stages, err)
			}
		})
	}
}
