package file

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyCompletionFailures(t *testing.T) {
	for name, failWrite := range map[string]bool{
		"given a close-only failure, when copying, then retain the live tree and active snapshot":     false,
		"given copy and close failures, when copying, then preserve both causes and recovery options": true,
	} {
		t.Run(name, func(t *testing.T) {
			parent, backups := restoreTempDir(t), t.TempDir()
			t.Setenv("TMPDIR", backups)
			source := filepath.Join(parent, "server")
			saved := map[string]string{"payload": "saved bytes"}
			live := map[string]string{"payload": "live replacement bytes"}
			writeRestoreTree(t, source, saved)
			if err := os.Chmod(filepath.Join(source, "payload"), 0o751); err != nil {
				t.Fatal(err)
			}
			b := NewBackup().(*backup)
			if err := b.Create(source); err != nil {
				t.Fatal(err)
			}
			snapshot := *b.Path()
			writeRestoreTree(t, source, live)
			closeErr := errors.New("destination close failed")
			var writeErr error
			if failWrite {
				writeErr = errors.New("destination write failed")
			}
			closes := 0
			copySnapshot := func(source, destination string) error {
				return copyFileWithDestination(filepath.Join(source, "payload"), filepath.Join(destination, "payload"),
					func(path string) (io.WriteCloser, error) {
						dst, err := os.Create(path)
						if err != nil {
							return nil, err
						}
						return &completionFailureFile{file: dst, writeErr: writeErr, closeErr: closeErr, closes: &closes}, nil
					})
			}
			for i, operation := range []struct {
				run      func() error
				sentinel error
			}{
				{func() error { return b.create(source, copySnapshot) }, ErrBackupCreateFailed},
				{func() error {
					return b.restore(source, restoreOperations{copy: copySnapshot, rename: os.Rename, removeAll: os.RemoveAll})
				}, ErrBackupRestoreFailed},
			} {
				err := operation.run()
				for _, cause := range []error{operation.sentinel, ErrFileCopyFailed, closeErr, writeErr} {
					if cause != nil && !errors.Is(err, cause) {
						t.Fatalf("expected copy completion error retaining %v, got %v", cause, err)
					}
				}
				if closes != i+1 {
					t.Errorf("destination close calls = %d, want %d", closes, i+1)
				}
				if b.Path() == nil || *b.Path() != snapshot {
					t.Fatal("incomplete copy changed the active snapshot")
				}
				assertRestoreTree(t, snapshot, saved)
				assertRestoreTree(t, source, live)
				if entries, err := os.ReadDir(backups); err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(snapshot) {
					t.Errorf("incomplete snapshot was not cleaned up: %v, %v", entries, err)
				}
				if entries, err := os.ReadDir(parent); err != nil || len(entries) != 1 || entries[0].Name() != "server" {
					t.Errorf("incomplete restoration was not cleaned up: %v, %v", entries, err)
				}
			}
			if err := b.Restore(source); err != nil {
				t.Fatalf("retry through the public API failed: %v", err)
			}
			assertRestoreTree(t, source, saved)
			if b.Path() != nil {
				t.Error("successful retry did not consume the snapshot")
			}
			if info, err := os.Stat(filepath.Join(source, "payload")); err != nil || info.Mode().Perm() != 0o751 {
				t.Errorf("successful copy did not preserve the file mode: %v, %v", info, err)
			}
		})
	}
}

type completionFailureFile struct {
	// Embedding os.File would let io.Copy bypass the injected Write failure.
	file     *os.File
	writeErr error
	closeErr error
	closes   *int
}

func (f *completionFailureFile) Write(data []byte) (int, error) {
	if f.writeErr == nil {
		return f.file.Write(data)
	}
	n, err := f.file.Write(data[:min(len(data), 3)])
	return n, errors.Join(err, f.writeErr)
}

func (f *completionFailureFile) Close() error {
	(*f.closes)++
	return errors.Join(f.file.Close(), f.closeErr)
}
