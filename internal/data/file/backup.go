package file

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	ErrBackupCreateFailed  = errors.New("unable to create backup")
	ErrBackupDeleteFailed  = errors.New("unable to delete backup")
	ErrBackupMissing       = errors.New("backup is missing")
	ErrBackupRestoreFailed = errors.New("unable to restore backup")
)

// Backup provides an interface for all file backup related operations
type Backup interface {
	// Creates a complete snapshot of the source directory. A failed call retains
	// any previous backup.
	Create(source string) error

	// Removes the current backup directory + files
	Remove() error

	// Restores the saved backup to the given destination, then delete the backup
	Restore(destination string) error

	// Returns the location of the current backup
	Path() *string
}

type backup struct {
	location *string
}

func NewBackup() Backup {
	return &backup{}
}

func (b *backup) Create(source string) error {
	tmp, err := os.MkdirTemp("", "warden-backup")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBackupCreateFailed, err)
	}

	err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(tmp, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}
		if err := copyFile(path, dstPath); err != nil {
			return fmt.Errorf("copy %q: %w", path, err)
		}
		return nil
	})
	if err != nil {
		err = fmt.Errorf("%w: %w", ErrBackupCreateFailed, err)
		if cleanupErr := os.RemoveAll(tmp); cleanupErr != nil {
			return errors.Join(err, fmt.Errorf("%w at %q: %w", ErrBackupDeleteFailed, tmp, cleanupErr))
		}
		return err
	}

	b.location = &tmp
	return nil
}

func (b *backup) Restore(destination string) error {
	if b.location == nil {
		return ErrBackupMissing
	}

	// Remove existing files at destination
	if err := os.RemoveAll(destination); err != nil {
		return ErrBackupRestoreFailed
	}
	if err := os.MkdirAll(destination, os.ModePerm); err != nil {
		return ErrBackupRestoreFailed
	}

	// Move backed up files to destination
	if err := moveFiles(*b.location, destination); err != nil {
		return ErrBackupRestoreFailed
	}

	// Delete back-up once successfuly moved over
	if err := os.RemoveAll(*b.location); err != nil {
		return ErrBackupDeleteFailed
	}
	b.location = nil
	return nil
}

func (b *backup) Remove() error {
	if b.location == nil {
		return ErrBackupMissing
	}
	if err := os.RemoveAll(*b.location); err != nil {
		return ErrBackupDeleteFailed
	}
	b.location = nil
	return nil
}

func (b *backup) Path() *string {
	return b.location
}
