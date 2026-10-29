package file_test

import (
	"errors"
	"github.com/ddoyle2017/warden/internal/data/file"
	"github.com/ddoyle2017/warden/internal/test/helper"
	"os"
	"path/filepath"
	"testing"
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

func TestRestoreDestinations(t *testing.T) {
	tests := map[string]struct {
		missing       bool
		missingParent bool
		emptyTarget   bool
		emptySnapshot bool
		symlinkParent bool
	}{
		"given a populated destination, when restoring, then replace its entire file set":         {},
		"given an empty destination, when restoring, then populate the complete tree":             {emptyTarget: true},
		"given a missing destination, when restoring, then create the complete tree":              {missing: true},
		"given missing parents, when restoring, then create the complete tree":                    {missing: true, missingParent: true},
		"given an empty snapshot, when restoring, then remove live files and leave a directory":   {emptySnapshot: true},
		"given a symlinked parent, when restoring, then safely publish in the resolved directory": {symlinkParent: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			source, parent, backups := th.GetValheimDirectory(), t.TempDir(), t.TempDir()
			t.Setenv("TMPDIR", backups)
			if !tt.emptySnapshot {
				if err := os.MkdirAll(filepath.Join(source, "nested", "empty"), 0o750); err != nil {
					t.Fatal(err)
				}
				executable := filepath.Join(source, "nested", "plugin")
				if err := os.WriteFile(executable, []byte("original plugin bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(executable, 0o751); err != nil {
					t.Fatal(err)
				}
			}
			destination := filepath.Join(parent, "server")
			if tt.missingParent {
				destination = filepath.Join(parent, "missing", "parent", "server")
			}
			if !tt.missing {
				if err := os.Mkdir(destination, 0o755); err != nil {
					t.Fatal(err)
				}
				if !tt.emptyTarget {
					if err := os.WriteFile(filepath.Join(destination, "live-only"), []byte("live bytes"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tt.symlinkParent {
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(parent, alias); err != nil {
					t.Fatal(err)
				}
				destination = filepath.Join(alias, "server")
			}
			b := file.NewBackup()
			if err := b.Create(source); err != nil {
				t.Fatal(err)
			}
			if err := b.Restore(destination); err != nil {
				t.Fatal(err)
			}
			th.VerifyBackup(source, destination)
			if info, err := os.Stat(destination); err != nil || !info.IsDir() {
				t.Fatalf("restored destination is not a directory: %v", err)
			}
			if !tt.emptySnapshot {
				info, err := os.Stat(filepath.Join(destination, "nested", "plugin"))
				if err != nil || info.Mode().Perm() != 0o751 {
					t.Errorf("restored executable lost its mode: %v, %v", info, err)
				}
				if info, err := os.Stat(filepath.Join(destination, "nested", "empty")); err != nil || !info.IsDir() {
					t.Errorf("restoration lost the empty directory: %v", err)
				}
			}
			if b.Path() != nil {
				t.Error("successful restoration did not consume the backup")
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("backup cleanup left files: %v, %v", entries, err)
			}
			if entries, err := os.ReadDir(filepath.Dir(destination)); err != nil || len(entries) != 1 || entries[0].Name() != "server" {
				t.Errorf("restoration left staging or prior files: %v, %v", entries, err)
			}
		})
	}
}

func TestRestoreUnsafeDestinations(t *testing.T) {
	tests := map[string]func(root, snapshot string) string{
		"given a file target, when restoring, then reject it without mutation": func(root, _ string) string {
			return filepath.Join(root, "file")
		},
		"given a symlink target, when restoring, then reject it without mutation": func(root, _ string) string {
			return filepath.Join(root, "link")
		},
		"given the backup as target, when restoring, then reject it without mutation": func(_, snapshot string) string {
			return snapshot
		},
		"given a target below the backup, when restoring, then reject it before creating parents": func(_, snapshot string) string {
			return filepath.Join(snapshot, "missing", "child")
		},
		"given a target containing the backup, when restoring, then reject it without mutation": func(_, snapshot string) string {
			return filepath.Dir(snapshot)
		},
		"given a parent alias into the backup, when restoring, then reject the overlapping target": func(root, _ string) string {
			return filepath.Join(root, "alias", "child")
		},
	}
	for name, target := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			root := t.TempDir()
			t.Setenv("TMPDIR", t.TempDir())
			if err := os.WriteFile(filepath.Join(th.GetValheimDirectory(), "saved"), []byte("snapshot bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			b := file.NewBackup()
			if err := b.Create(th.GetValheimDirectory()); err != nil {
				t.Fatal(err)
			}
			snapshot := *b.Path()
			live := filepath.Join(root, "live")
			if err := os.Mkdir(live, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{filepath.Join(root, "file"), filepath.Join(live, "sentinel")} {
				if err := os.WriteFile(path, []byte("live bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			links := map[string]string{"link": live, "alias": snapshot}
			for name, target := range links {
				if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
					t.Fatal(err)
				}
			}
			before, err := th.GetFileList(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := b.Restore(target(root, snapshot)); !errors.Is(err, file.ErrBackupRestoreFailed) {
				t.Fatalf("expected unsafe destination failure, got %v", err)
			}
			if b.Path() == nil || *b.Path() != snapshot {
				t.Fatal("invalid destination changed the active backup")
			}
			th.VerifyBackup(th.GetValheimDirectory(), snapshot)
			entries, err := os.ReadDir(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "saved" || entries[0].IsDir() {
				t.Errorf("invalid restoration created snapshot directories: %v", entries)
			}
			after, err := th.GetFileList(root)
			if err != nil {
				t.Fatal(err)
			}
			if !th.AreFileListsEqual(before, after) {
				t.Error("invalid restoration changed the live file set")
			}
			for _, path := range []string{filepath.Join(root, "file"), filepath.Join(live, "sentinel")} {
				if data, err := os.ReadFile(path); err != nil || string(data) != "live bytes" {
					t.Errorf("invalid restoration changed live bytes: %q, %v", data, err)
				}
			}
			for name, target := range links {
				if got, err := os.Readlink(filepath.Join(root, name)); err != nil || got != target {
					t.Errorf("invalid restoration changed symlink %q: %q, %v", name, got, err)
				}
			}
		})
	}
}
