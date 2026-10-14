package file_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/data/file"
	"warden/internal/test/helper"
)

func TestCreate_RetainsSnapshotWhenCopyAndCleanupFail(t *testing.T) {
	th := helper.NewHelper(t)
	backups := t.TempDir()
	t.Setenv("TMPDIR", backups)
	source := th.GetValheimDirectory()
	for _, name := range []string{"a-copied-first", "z-sentinel"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte("original bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cleanupFailure := errors.New("incomplete snapshot cleanup failed")
	var incomplete string
	b := file.NewBackupWithIncompleteCleanup(func(path string) error {
		incomplete = path
		return cleanupFailure
	})
	if err := b.Create(source); err != nil {
		t.Fatal(err)
	}
	if b.Path() == nil {
		t.Fatal("expected the initial snapshot to be active")
	}
	prior := *b.Path()
	if err := os.WriteFile(filepath.Join(source, "a-copied-first"), []byte("replacement bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(source, "missing"), filepath.Join(source, "b-dangling")); err != nil {
		t.Fatal(err)
	}

	err := b.Create(source)
	for _, cause := range []error{file.ErrBackupCreateFailed, file.ErrFileOpenFailed, file.ErrBackupDeleteFailed, cleanupFailure} {
		if !errors.Is(err, cause) {
			t.Errorf("expected creation error to retain %v, got %v", cause, err)
		}
	}
	if incomplete == "" || err == nil || !strings.Contains(err.Error(), incomplete) {
		t.Fatalf("expected the failed cleanup path in the diagnostic, path %q, error %v", incomplete, err)
	}
	if b.Path() == nil || *b.Path() != prior {
		t.Fatal("failed copy and cleanup replaced the active snapshot")
	}
	if data, err := os.ReadFile(filepath.Join(incomplete, "a-copied-first")); err != nil || string(data) != "replacement bytes" {
		t.Errorf("expected a retained partial copy, got %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(incomplete, "z-sentinel")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected the partial copy to omit the later file, got %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(source, "a-copied-first")); err != nil || string(data) != "replacement bytes" {
		t.Errorf("failed creation changed live bytes: %q, %v", data, err)
	}
	if err := b.Restore(source); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a-copied-first", "z-sentinel"} {
		if data, err := os.ReadFile(filepath.Join(source, name)); err != nil || string(data) != "original bytes" {
			t.Errorf("previous snapshot did not restore %s: %q, %v", name, data, err)
		}
	}
	if entries, err := os.ReadDir(backups); err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(incomplete) {
		t.Errorf("expected only the diagnosed incomplete snapshot to remain, got %v, %v", entries, err)
	}
}
