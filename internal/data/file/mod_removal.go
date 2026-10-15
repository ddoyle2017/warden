package file

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ModRemoval keeps staged files recoverable until the caller commits or rolls back.
// Commit is irreversible, even when it reports a cleanup error.
type ModRemoval interface {
	Commit() error
	Rollback() error
}

var ErrModRemovalFinished = errors.New("mod removal is already finished")

// ModRemovalCleanupError reports cleanup failure after files are removed from the
// live installation. Callers must not treat it as a failed removal or roll it back.
// File-only removals return it directly; nested markers do not establish completion.
type ModRemovalCleanupError struct {
	Err error
}

func (e *ModRemovalCleanupError) Error() string {
	return fmt.Sprintf("mod files removed; cleanup failed: %v", e.Err)
}

func (e *ModRemovalCleanupError) Unwrap() error {
	return e.Err
}

type modRemoval struct {
	backup       Backup
	target       string
	staging      string
	recreated    bool
	finished     bool
	removeStaged func(string) error
}

func (m *manager) stageModRemoval(target string, recreate bool, operationErr error) (ModRemoval, error) {
	r := &modRemoval{backup: NewBackup(), target: target, removeStaged: os.RemoveAll}
	if err := r.backup.Create(m.modDirectory); err != nil {
		return nil, fmt.Errorf("%w: %w", operationErr, err)
	}

	// Keep staged DLLs outside plugins. A cross-filesystem rename fails before metadata changes.
	staging, err := os.MkdirTemp(filepath.Dir(m.modDirectory), ".warden-removal-")
	if err != nil {
		return nil, errors.Join(fmt.Errorf("%w: %w", operationErr, err), r.cleanup())
	}
	r.staging = staging
	if err := os.Rename(target, filepath.Join(staging, "contents")); err != nil {
		return nil, errors.Join(fmt.Errorf("%w: %w", operationErr, err), r.cleanup())
	}
	if recreate {
		if err := os.MkdirAll(target, os.ModePerm); err != nil {
			return nil, errors.Join(fmt.Errorf("%w: %w", ErrDirectoryCreateFailed, err), r.Rollback())
		}
		r.recreated = true
	}
	return r, nil
}

func (r *modRemoval) Commit() error {
	if r.finished {
		return ErrModRemovalFinished
	}
	r.finished = true
	if err := r.cleanup(); err != nil {
		return &ModRemovalCleanupError{Err: err}
	}
	return nil
}

func (r *modRemoval) cleanup() error {
	if r.backup == nil {
		return nil
	}
	if r.backup.Path() == nil {
		return ErrBackupMissing
	}
	var stagingErr error
	if r.staging != "" {
		if err := r.removeStaged(r.staging); err != nil {
			stagingErr = fmt.Errorf("remove staged mods at %q: %w", r.staging, err)
		}
	}
	snapshot := *r.backup.Path()
	var backupErr error
	if err := r.backup.Remove(); err != nil {
		backupErr = fmt.Errorf("remove snapshot at %q: %w", snapshot, err)
	}
	return errors.Join(stagingErr, backupErr)
}

func (r *modRemoval) Rollback() error {
	if r.finished {
		return ErrModRemovalFinished
	}
	if r.backup == nil {
		r.finished = true
		return nil
	}
	if r.backup.Path() == nil {
		return ErrBackupMissing
	}
	if info, err := os.Lstat(r.target); err == nil {
		if !r.recreated || !info.IsDir() {
			return fmt.Errorf("%w; original mods remain at %q: %w", ErrBackupRestoreFailed, r.staging, os.ErrExist)
		}
		// Remove only the empty directory created by staging, not new plugin data.
		if err := os.Remove(r.target); err != nil {
			return fmt.Errorf("%w; original mods remain at %q: %w", ErrBackupRestoreFailed, r.staging, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w; original mods remain at %q: %w", ErrBackupRestoreFailed, r.staging, err)
	}
	if err := os.Rename(filepath.Join(r.staging, "contents"), r.target); err != nil {
		return fmt.Errorf("%w; original mods remain at %q: %w", ErrBackupRestoreFailed, r.staging, err)
	}
	r.finished = true
	if err := r.cleanup(); err != nil {
		return fmt.Errorf("mod files restored; cleanup failed: %w", err)
	}
	return nil
}
