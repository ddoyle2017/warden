package file

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"warden/internal/api"
)

// ModReplacement owns the original files outside plugins after publication.
// Commit is irreversible and must follow a confirmed metadata write. An unknown
// write outcome must leave the handle uncommitted and report RecoveryPath.
type ModReplacement interface {
	Path() string
	// Empty if the original was absent or Commit has consumed recovery state.
	RecoveryPath() string
	// A direct ModInstallCleanupError reports completed publication with cleanup
	// leftovers, not a usable original or permission to roll back.
	Commit() error
}

var ErrModReplacementFinished = errors.New("mod replacement is already finished")

type modReplacement struct {
	path        string
	staging     string
	hadOriginal bool
	finished    bool
	removeAll   func(string) error
}

type modReplacementOperations struct {
	rename        func(string, string) error
	removeArchive func(string) error
	removeAll     func(string) error
}

func (m *manager) ReplaceMod(url, oldFullName, newFullName string) (ModReplacement, error) {
	return m.replaceMod(url, oldFullName, newFullName, modReplacementOperations{
		rename: os.Rename, removeArchive: os.Remove, removeAll: os.RemoveAll,
	})
}

func (m *manager) replaceMod(url, oldFullName, newFullName string, ops modReplacementOperations) (_ ModReplacement, err error) {
	if err := m.ValidateModInstall(); err != nil {
		return nil, err
	}
	for _, name := range []string{oldFullName, newFullName} {
		if !filepath.IsLocal(name) || name == "." || strings.ContainsAny(name, `/\`) {
			return nil, fmt.Errorf("invalid mod release directory %q: %w", name, os.ErrInvalid)
		}
	}
	target := filepath.Join(m.modDirectory, oldFullName)
	destination := filepath.Join(m.modDirectory, newFullName)
	if err := requireAbsentModPath(destination); err != nil {
		return nil, fmt.Errorf("%w: replacement destination: %w", ErrFileRenameFailed, err)
	}
	if err := os.MkdirAll(filepath.Dir(m.modDirectory), os.ModePerm); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDirectoryCreateFailed, err)
	}
	// This sibling survives nested installs and keeps staged DLLs outside plugins.
	staging, err := os.MkdirTemp(filepath.Dir(m.modDirectory), ".warden-replacement-")
	if err != nil {
		return nil, fmt.Errorf("%w: stage mod replacement: %w", ErrDirectoryCreateFailed, err)
	}
	retain := false
	defer func() {
		if !retain {
			err = errors.Join(err, cleanupModReplacement(staging, ops.removeAll))
		}
	}()

	archive := filepath.Join(staging, "download.zip")
	if err := m.downloadModReplacement(url, archive); err != nil {
		return nil, err
	}
	prepared := filepath.Join(staging, "replacement")
	if err := Unzip(archive, prepared); err != nil {
		return nil, err
	}
	if err := ops.removeArchive(archive); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrZipDeleteFailed, err)
	}
	if err := os.MkdirAll(m.modDirectory, os.ModePerm); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDirectoryCreateFailed, err)
	}
	if err := requireAbsentModPath(destination); err != nil {
		return nil, fmt.Errorf("%w: replacement destination: %w", ErrFileRenameFailed, err)
	}
	info, statErr := os.Lstat(target)
	hadOriginal := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: inspect original %q: %w", ErrModDeleteFailed, target, statErr)
	}
	if hadOriginal && !info.IsDir() {
		return nil, fmt.Errorf("%w: original %q is not a directory", ErrModDeleteFailed, target)
	}
	original := filepath.Join(staging, "original")
	if hadOriginal {
		if err := ops.rename(target, original); err != nil {
			return nil, fmt.Errorf("%w: retain original %q: %w", ErrModDeleteFailed, target, err)
		}
	}
	if err := ops.rename(prepared, destination); err != nil {
		publishErr := fmt.Errorf("%w: publish replacement to %q: %w", ErrFileRenameFailed, destination, err)
		if hadOriginal {
			recoveryErr := requireAbsentModPath(target)
			if recoveryErr == nil {
				recoveryErr = ops.rename(original, target)
			}
			if recoveryErr != nil {
				retain = true
				return nil, errors.Join(publishErr, fmt.Errorf("%w: recover %q: %w; original retained at %q; prepared replacement at %q",
					ErrBackupRestoreFailed, target, recoveryErr, original, prepared))
			}
		}
		return nil, publishErr
	}

	retain = true
	replacement := &modReplacement{path: destination, staging: staging, hadOriginal: hadOriginal, removeAll: ops.removeAll}
	if !hadOriginal {
		replacement.staging = ""
		if err := cleanupModReplacement(staging, ops.removeAll); err != nil {
			return replacement, &ModInstallCleanupError{Err: err}
		}
	}
	return replacement, nil
}

func (m *manager) downloadModReplacement(url, archive string) (err error) {
	response, err := m.client.Get(url)
	if err != nil {
		return fmt.Errorf("%w: %w", api.ErrHTTPClient, err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close replacement download: %w", closeErr))
		}
	}()
	return createFile(archive, response.Body)
}

func requireAbsentModPath(path string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("refusing to overwrite %q: %w", path, os.ErrExist)
}

func (r *modReplacement) Path() string {
	return r.path
}

func (r *modReplacement) RecoveryPath() string {
	if r.finished || !r.hadOriginal {
		return ""
	}
	return filepath.Join(r.staging, "original")
}

func (r *modReplacement) Commit() error {
	if r.finished {
		return ErrModReplacementFinished
	}
	// Cleanup may partly delete the original, so consume ownership before starting.
	r.finished = true
	if r.staging == "" {
		return nil
	}
	if err := cleanupModReplacement(r.staging, r.removeAll); err != nil {
		return &ModInstallCleanupError{Err: err}
	}
	return nil
}

func cleanupModReplacement(staging string, removeAll func(string) error) error {
	if err := removeAll(staging); err != nil {
		return fmt.Errorf("%w: remove mod replacement staging %q: %w", ErrBackupDeleteFailed, staging, err)
	}
	return nil
}
