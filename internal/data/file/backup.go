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
	// Creates a complete snapshot and records the source root's permission mode.
	// A failed call retains the previous snapshot and its recorded root mode.
	Create(source string) error

	// Consumes the active backup before deleting its files. Cleanup failures
	// report leftovers, not a retryable snapshot.
	Remove() error

	// Restores a complete copy before consuming the backup. Failures before
	// publication retain the snapshot; cleanup failures report leftover paths.
	// Preserves existing destination permissions, or the saved source root's
	// permissions when the destination is missing.
	Restore(destination string) error

	// Returns the location of the current backup
	Path() *string
}

type backup struct {
	location         *string
	rootMode         os.FileMode
	removeIncomplete func(string) error
}

func NewBackup() Backup {
	return &backup{removeIncomplete: os.RemoveAll}
}

func (b *backup) Create(source string) error {
	return b.create(source, copyDirectory)
}

func (b *backup) create(source string, copySnapshot func(string, string) error) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("%w: inspect source %q: %w", ErrBackupCreateFailed, source, err)
	}
	tmp, err := os.MkdirTemp("", "warden-backup")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBackupCreateFailed, err)
	}

	if err := copySnapshot(source, tmp); err != nil {
		err = fmt.Errorf("%w: %w", ErrBackupCreateFailed, err)
		if cleanupErr := b.removeIncomplete(tmp); cleanupErr != nil {
			return errors.Join(err, fmt.Errorf("%w at %q: %w", ErrBackupDeleteFailed, tmp, cleanupErr))
		}
		return err
	}

	b.location, b.rootMode = &tmp, info.Mode()
	return nil
}

func (b *backup) Restore(destination string) error {
	return b.restore(destination, restoreOperations{
		copy:      copyDirectory,
		rename:    os.Rename,
		removeAll: os.RemoveAll,
	})
}

type restoreOperations struct {
	copy      func(string, string) error
	rename    func(string, string) error
	removeAll func(string) error
}

func (b *backup) restore(destination string, ops restoreOperations) (err error) {
	if b.location == nil {
		return ErrBackupMissing
	}
	snapshot, rootMode := *b.location, b.rootMode

	destination, err = restoreDestination(snapshot, destination)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBackupRestoreFailed, err)
	}
	info, statErr := os.Lstat(destination)
	hadDestination := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("%w: inspect %q: %w", ErrBackupRestoreFailed, destination, statErr)
	}
	if hadDestination {
		rootMode = info.Mode()
	}
	if err := os.MkdirAll(filepath.Dir(destination), os.ModePerm); err != nil {
		return fmt.Errorf("%w: prepare parent of %q: %w", ErrBackupRestoreFailed, destination, err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(destination), ".warden-restore-")
	if err != nil {
		return fmt.Errorf("%w: stage beside %q: %w", ErrBackupRestoreFailed, destination, err)
	}
	preserveStaging, published := false, false
	defer func() {
		if preserveStaging {
			return
		}
		if cleanupErr := ops.removeAll(staging); cleanupErr != nil {
			action := "remove restore staging"
			if published {
				action = "remove prior tree after successful restoration"
			}
			err = errors.Join(err, fmt.Errorf("%w: %s %q: %w", ErrBackupDeleteFailed, action, staging, cleanupErr))
		}
	}()

	replacement := filepath.Join(staging, "replacement")
	if err := os.Mkdir(replacement, os.ModePerm); err != nil {
		return fmt.Errorf("%w: prepare %q: %w", ErrBackupRestoreFailed, replacement, err)
	}
	if err := ops.copy(snapshot, replacement); err != nil {
		return fmt.Errorf("%w: copy backup %q: %w", ErrBackupRestoreFailed, snapshot, err)
	}
	// The published root's permissions are independent of the private containers.
	if err := os.Chmod(replacement, rootMode); err != nil {
		return fmt.Errorf("%w: set replacement permissions for %q: %w", ErrBackupRestoreFailed, destination, err)
	}

	previous := filepath.Join(staging, "previous")
	if hadDestination {
		if err := ops.rename(destination, previous); err != nil {
			return fmt.Errorf("%w: retain prior tree %q: %w", ErrBackupRestoreFailed, destination, err)
		}
	}
	if err := ops.rename(replacement, destination); err != nil {
		publishErr := fmt.Errorf("%w: publish to %q: %w", ErrBackupRestoreFailed, destination, err)
		if hadDestination {
			if recoveryErr := ops.rename(previous, destination); recoveryErr != nil {
				preserveStaging = true
				return errors.Join(publishErr, fmt.Errorf("recover %q from %q: %w; replacement retained at %q, backup at %q",
					destination, previous, recoveryErr, replacement, snapshot))
			}
		}
		return publishErr
	}

	published = true
	// Cleanup can partially delete the snapshot, so it is no longer retryable.
	b.location, b.rootMode = nil, 0
	if err := ops.removeAll(snapshot); err != nil {
		return fmt.Errorf("%w: restored %q; remove consumed backup %q: %w", ErrBackupDeleteFailed, destination, snapshot, err)
	}
	return nil
}

func restoreDestination(snapshot, destination string) (string, error) {
	destination, err := restoreTarget(destination)
	if err != nil {
		return "", err
	}
	source, err := filepath.Abs(snapshot)
	if err != nil {
		return "", err
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return "", err
	}
	for _, paths := range [][2]string{{source, destination}, {destination, source}} {
		overlap, err := directoryContains(paths[0], paths[1])
		if err != nil {
			return "", err
		}
		if overlap {
			return "", fmt.Errorf("restore destination %q overlaps backup %q", destination, source)
		}
	}
	return destination, nil
}

func preflightRestore(destination string) error {
	if _, err := restoreTarget(destination); err != nil {
		return fmt.Errorf("%w: restore preflight: %w", ErrBackupRestoreFailed, err)
	}
	return nil
}

func restoreTarget(destination string) (string, error) {
	clean := filepath.Clean(destination)
	if destination == "" || clean == "." || clean == ".." {
		return "", fmt.Errorf("unsafe restore destination %q", destination)
	}
	destination, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(destination); err == nil && !info.IsDir() {
		return "", fmt.Errorf("restore destination %q is not a directory", destination)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	// Resolve existing ancestors without creating missing parents or following
	// a destination symlink. This also catches aliases that overlap the backup.
	existing := destination
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		existing = filepath.Dir(existing)
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	suffix, err := filepath.Rel(existing, destination)
	if err != nil {
		return "", err
	}
	destination = filepath.Join(resolved, suffix)
	if destination == filepath.Dir(destination) {
		return "", fmt.Errorf("cannot restore over filesystem root %q", destination)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", err
	}
	containsWorkingDirectory, err := directoryContains(destination, cwd)
	if err != nil {
		return "", err
	}
	if containsWorkingDirectory {
		return "", fmt.Errorf("restore destination %q contains the working directory", destination)
	}
	return destination, nil
}

func directoryContains(parent, child string) (bool, error) {
	parentInfo, err := os.Stat(parent)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for {
		info, err := os.Stat(child)
		if err == nil && os.SameFile(parentInfo, info) {
			return true, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		next := filepath.Dir(child)
		if next == child {
			return false, nil
		}
		child = next
	}
}

func (b *backup) Remove() error {
	return b.remove(os.RemoveAll)
}

func (b *backup) remove(removeAll func(string) error) error {
	if b.location == nil {
		return ErrBackupMissing
	}
	snapshot := *b.location
	b.location, b.rootMode = nil, 0
	if err := removeAll(snapshot); err != nil {
		return fmt.Errorf("%w: remove consumed backup %q: %w", ErrBackupDeleteFailed, snapshot, err)
	}
	return nil
}

func (b *backup) Path() *string {
	return b.location
}
