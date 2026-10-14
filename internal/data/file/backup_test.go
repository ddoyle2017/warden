package file_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"warden/internal/data/file"
	"warden/internal/test/helper"
)

func TestCreate_Happy(t *testing.T) {
	tests := map[string]bool{
		"given an empty directory, when creating a backup, then publish an empty snapshot":           false,
		"given installed files, when creating a backup, then copy every file and preserve its bytes": true,
	}

	for name, populated := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			if populated {
				th.SetUpServerFiles()
			}
			t.Setenv("TMPDIR", t.TempDir())
			b := file.NewBackup()

			if err := b.Create(th.GetValheimDirectory()); err != nil {
				t.Fatal(err)
			}
			if b.Path() == nil {
				t.Fatal("expected a valid backup path, received nil")
			}
			th.VerifyBackup(th.GetValheimDirectory(), *b.Path())
		})
	}
}

func TestCreate_Failure(t *testing.T) {
	tests := map[string]struct {
		missingSource bool
		priorBackup   bool
		cause         error
	}{
		"given a missing source, when creating a backup, then leave no usable snapshot": {
			missingSource: true,
			cause:         os.ErrNotExist,
		},
		"given a partial copy failure, when creating a backup, then leave no usable snapshot": {
			cause: file.ErrFileOpenFailed,
		},
		"given a valid backup, when a replacement copy fails, then retain the original snapshot": {
			priorBackup: true,
			cause:       file.ErrFileOpenFailed,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			source := th.GetValheimDirectory()
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			for _, name := range []string{"a-copied-first", "z-sentinel"} {
				if err := os.WriteFile(filepath.Join(source, name), []byte("original bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			b := file.NewBackup()
			var priorPath string
			if tt.priorBackup {
				if err := b.Create(source); err != nil {
					t.Fatal(err)
				}
				if b.Path() == nil {
					t.Fatal("expected a valid backup")
				}
				priorPath = *b.Path()
			}
			sentinel := filepath.Join(source, "z-sentinel")
			if err := os.WriteFile(sentinel, []byte("live bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			if tt.missingSource {
				source = filepath.Join(source, "missing")
			} else if err := os.Symlink(filepath.Join(source, "missing"), filepath.Join(source, "b-dangling")); err != nil {
				t.Fatal(err)
			}

			err := b.Create(source)
			if !errors.Is(err, file.ErrBackupCreateFailed) || !errors.Is(err, tt.cause) {
				t.Errorf("expected backup creation failure wrapping %v, got %v", tt.cause, err)
			}
			if priorPath == "" {
				if b.Path() != nil {
					t.Errorf("failed creation published a snapshot at %s", *b.Path())
				}
			} else if b.Path() == nil || *b.Path() != priorPath {
				t.Error("failed replacement changed the active backup")
			}

			entries, err := os.ReadDir(backups)
			if err != nil {
				t.Fatal(err)
			}
			if priorPath == "" && len(entries) != 0 {
				t.Errorf("incomplete snapshot was not cleaned up: %v", entries)
			}
			if priorPath != "" && (len(entries) != 1 || entries[0].Name() != filepath.Base(priorPath)) {
				t.Errorf("expected only the original snapshot to remain, got %v", entries)
			}
			if data, err := os.ReadFile(sentinel); err != nil || string(data) != "live bytes" {
				t.Errorf("failed creation changed live data: %q, %v", data, err)
			}

			expectedErr := file.ErrBackupMissing
			expectedBytes := "live bytes"
			if tt.priorBackup {
				expectedErr = nil
				expectedBytes = "original bytes"
			}
			if err := b.Restore(th.GetValheimDirectory()); !errors.Is(err, expectedErr) {
				t.Errorf("expected restore error %v, got %v", expectedErr, err)
			}
			if data, err := os.ReadFile(sentinel); err != nil || string(data) != expectedBytes {
				t.Errorf("unexpected sentinel after restore: %q, %v", data, err)
			}
			if data, err := os.ReadFile(filepath.Join(th.GetValheimDirectory(), "a-copied-first")); err != nil || string(data) != "original bytes" {
				t.Errorf("unexpected early file after restore: %q, %v", data, err)
			}
			if !tt.missingSource && !tt.priorBackup {
				link := filepath.Join(source, "b-dangling")
				if target, err := os.Readlink(link); err != nil || target != filepath.Join(source, "missing") {
					t.Errorf("failed restore changed the live symlink: %q, %v", target, err)
				}
			}
		})
	}
}

func TestRemove_Happy(t *testing.T) {
	th := helper.NewHelper(t)
	th.SetUpServerFiles()

	b := file.NewBackup()
	if err := b.Create(th.GetValheimDirectory()); err != nil {
		t.Errorf("unexpected error creating backup, received: %+v", err)
	}
	if err := b.Remove(); err != nil {
		t.Errorf("unexpected error removing backup, received: %+v", err)
	}
	if b.Path() != nil {
		t.Errorf("expected backup path to be nil, received: %s", *b.Path())
	}

	t.Cleanup(func() {
		th.RemoveServerFiles()
	})
}

func TestRemove_Sad(t *testing.T) {
	th := helper.NewHelper(t)
	th.SetUpServerFiles()

	tests := map[string]struct {
		setup    func(b file.Backup)
		expected error
	}{
		"returns error when there is no backup": {
			setup:    func(b file.Backup) {},
			expected: file.ErrBackupMissing,
		},
		"returns nil when registered backup is missing, then clears backup path": {
			setup: func(b file.Backup) {
				// Create a valid backup
				if err := b.Create(th.GetValheimDirectory()); err != nil {
					t.Errorf("unexpected error creating backup, received: %+v", err)
				}
				// Remove the files without updating the Backup struct
				if err := os.RemoveAll(*b.Path()); err != nil {
					t.Errorf("unexpected error setting up test, received: %+v", err)
				}
			},
			expected: nil,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b := file.NewBackup()

			tt.setup(b)

			err := b.Remove()
			if !errors.Is(err, tt.expected) {
				t.Errorf("expected error: %+v, received: %+v", tt.expected, err)
			}
		})
	}

	t.Cleanup(func() {
		th.RemoveServerFiles()
	})
}

func TestRestore_Happy(t *testing.T) {
	th := helper.NewHelper(t)
	th.SetUpServerFiles()
	t.Setenv("TMPDIR", t.TempDir())
	sentinel := filepath.Join(th.GetValheimDirectory(), "server-data")
	if err := os.WriteFile(sentinel, []byte("original server bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create backup
	b := file.NewBackup()
	if err := b.Create(th.GetValheimDirectory()); err != nil {
		t.Fatalf("unexpected error creating backup, received: %+v", err)
	}

	// Verify backup is correct
	if b.Path() == nil {
		t.Fatal("expected a valid backup path, received nil")
	}
	th.VerifyBackup(th.GetValheimDirectory(), *b.Path())

	// Save a list of the original files
	original, err := th.GetFileList(th.GetValheimDirectory())
	if err != nil {
		t.Fatalf("unexpected error retrieving file list, received: %+v", err)
	}
	if err := os.WriteFile(sentinel, []byte("changed server bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(th.GetValheimDirectory(), "added-after-backup"), []byte("new file"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Restore backup
	if err := b.Restore(th.GetValheimDirectory()); err != nil {
		t.Fatalf("received an error when restoring backup, received: %+v", err)
	}
	if b.Path() != nil {
		t.Errorf("expected backup path to be nil, received: %s", *b.Path())
	}

	// Verify current file list is the same as the original set
	current, err := th.GetFileList(th.GetValheimDirectory())
	if err != nil {
		t.Errorf("unexpected error retrieving file list, received: %+v", err)
	}

	if !th.AreFileListsEqual(original, current) {
		t.Error("restored backup has a different file list than the original")
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "original server bytes" {
		t.Errorf("restore did not recover the original bytes: %q, %v", data, err)
	}

	t.Cleanup(func() {
		th.RemoveServerFiles()
	})
}

func TestRestore_Sad(t *testing.T) {
	tests := map[string]struct {
		createBackup       bool
		invalidDestination bool
		expected           error
	}{
		"given no backup, when restoring, then return a missing backup error": {
			expected: file.ErrBackupMissing,
		},
		"given a file as the destination parent, when restoring, then retain the backup": {
			createBackup:       true,
			invalidDestination: true,
			expected:           file.ErrBackupRestoreFailed,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			th.SetUpServerFiles()
			b := file.NewBackup()
			t.Cleanup(func() {
				if b.Path() != nil {
					if err := b.Remove(); err != nil {
						t.Errorf("unable to clean up backup: %v", err)
					}
				}
			})
			if tt.createBackup {
				if err := b.Create(th.GetValheimDirectory()); err != nil {
					t.Fatal(err)
				}
			}
			destination := filepath.Join(t.TempDir(), "restore")
			if tt.invalidDestination {
				parent := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
				destination = filepath.Join(parent, "restore")
			}

			err := b.Restore(destination)
			if !errors.Is(err, tt.expected) {
				t.Errorf("expected error: %+v, received: %+v", tt.expected, err)
			}
			if tt.createBackup {
				if b.Path() == nil {
					t.Fatal("expected failed restore to retain the backup")
				}
				th.VerifyBackup(th.GetValheimDirectory(), *b.Path())
			}
		})
	}
}
