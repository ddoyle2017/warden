package file

import (
	"errors"
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
	// Creates a copy of the source directory and files, then stores it as a backup
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
		return ErrBackupCreateFailed
	}
	b.location = &tmp

	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return ErrBackupCreateFailed
		}

		// Create the destination path
		relPath, err := filepath.Rel(source, path)
		if err != nil {
			return ErrBackupCreateFailed
		}
		dstPath := filepath.Join(tmp, relPath)

		if info.IsDir() {
			// Create the directory in the destination path
			if err := os.MkdirAll(dstPath, info.Mode()); err != nil {
				return ErrBackupCreateFailed
			}
		} else {
			// Copy the file to the destination path
			if err := copyFile(path, dstPath); err != nil {
				return ErrBackupCreateFailed
			}
		}
		return nil
	})
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
