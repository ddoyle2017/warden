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
	th := helper.NewHelper(t)
	th.SetUpServerFiles()

	b := file.NewBackup()

	if err := b.Create(th.GetValheimDirectory()); err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}
	if b.Path() == nil {
		t.Error("expected a valid backup path, received nil")
	}
	th.VerifyBackup(th.GetValheimDirectory(), *b.Path())

	t.Cleanup(func() {
		th.RemoveServerFiles()
		if err := os.RemoveAll(*b.Path()); err != nil {
			t.Errorf("unexpected error during backup clean-up, err: %+v", err)
		}
	})
}

func TestCreate_Sad(t *testing.T) {
	b := file.NewBackup()

	err := b.Create(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Errorf("unexpected a non-nil error, received nil")
	}
	if !errors.Is(err, file.ErrBackupCreateFailed) {
		t.Errorf("expected error: %+v, received: %+v", file.ErrBackupCreateFailed, err)
	}

	t.Cleanup(func() {
		if err := os.RemoveAll(*b.Path()); err != nil {
			t.Errorf("unexpected error during backup clean-up, err: %+v", err)
		}
	})
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

	// Create backup
	b := file.NewBackup()
	if err := b.Create(th.GetValheimDirectory()); err != nil {
		t.Errorf("unexpected error creating backup, received: %+v", err)
	}

	// Verify backup is correct
	if b.Path() == nil {
		t.Error("expected a valid backup path, received nil")
	}
	th.VerifyBackup(th.GetValheimDirectory(), *b.Path())

	// Save a list of the original files
	original, err := th.GetFileList(th.GetValheimDirectory())
	if err != nil {
		t.Errorf("unexpected error retrieving file list, received: %+v", err)
	}

	// Restore backup
	if err := b.Restore(th.GetValheimDirectory()); err != nil {
		t.Errorf("received an error when restoring backup, received: %+v", err)
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
