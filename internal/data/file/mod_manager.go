package file

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"warden/internal/api"
)

// An interface for all mod file operations
type modManager interface {
	// Checks the installation target without changing files.
	ValidateModInstall() error

	// Downloads the targetted mod, unzips it, and adds it to the mod
	// folder.
	//
	// URL is the download link for a specific release.
	// FullName is the namespace + mod name + version string that Thunderstore provides.
	// A direct ModInstallCleanupError returns the published path, not a failed installation.
	InstallMod(url, fullName string) (string, error)

	// Deletes the folder and contents for a mod. `FullName` is a
	// value provided by Thunderstore that contains the name, namespace, and version of a
	// specific mod release.
	RemoveMod(fullName string) error

	// Deletes the parent mod folder and all of its contents, then recreates an empty one.
	RemoveAllMods() error

	// Stages removal while retaining the original files until Commit or Rollback.
	StageModRemoval(fullName string) (ModRemoval, error)
	StageAllModsRemoval() (ModRemoval, error)
}

// ModInstallCleanupError reports completed file installation with failed cleanup.
// Only a direct return establishes completion; a nested marker is just a cause.
// Metadata and dependencies are the caller's responsibility.
type ModInstallCleanupError struct {
	Err error
}

func (e *ModInstallCleanupError) Error() string {
	return fmt.Sprintf("mod files installed; cleanup failed: %v", e.Err)
}

func (e *ModInstallCleanupError) Unwrap() error {
	return e.Err
}

func (m *manager) ValidateModInstall() error {
	return preflightRestore(m.modDirectory)
}

func (m *manager) InstallMod(url, fullName string) (string, error) {
	return m.installMod(url, fullName, os.Remove)
}

func (m *manager) installMod(url, fullName string, removeArchive func(string) error) (string, error) {
	if err := m.ValidateModInstall(); err != nil {
		return "", err
	}

	// Get the data
	resp, err := m.client.Get(url)
	if err != nil {
		return "", fmt.Errorf("%w: %w", api.ErrHTTPClient, err)
	}
	defer resp.Body.Close()

	if err := os.MkdirAll(m.modDirectory, os.ModePerm); err != nil {
		return "", fmt.Errorf("%w: %w", ErrDirectoryCreateFailed, err)
	}
	if err := m.backup.Create(m.modDirectory); err != nil {
		return "", err
	}

	// Create the zip archive
	zipPath := filepath.Join(m.modDirectory, fullName+".zip")
	if err := createFile(zipPath, resp.Body); err != nil {
		return "", errors.Join(err, m.restoreBackup(m.modDirectory))
	}

	// Extract zip files into a new folder for the mod
	destination := filepath.Join(m.modDirectory, fullName)
	if err := Unzip(zipPath, destination); err != nil {
		return "", errors.Join(err, m.restoreBackup(m.modDirectory))
	}

	// Remove zip file after finishing extraction
	if err := removeArchive(zipPath); err != nil {
		return "", errors.Join(fmt.Errorf("%w: %w", ErrZipDeleteFailed, err), m.restoreBackup(m.modDirectory))
	}
	if err := m.backup.Remove(); err != nil {
		return destination, &ModInstallCleanupError{Err: err}
	}
	return destination, nil
}

func (m *manager) RemoveMod(fullName string) error {
	return m.removeMod(fullName, ModRemoval.Commit)
}

func (m *manager) removeMod(fullName string, commit func(ModRemoval) error) error {
	removal, err := m.StageModRemoval(fullName)
	if err != nil {
		return err
	}
	if err := commit(removal); err != nil {
		if cleanupErr, completed := err.(*ModRemovalCleanupError); completed {
			return &ModRemovalCleanupError{Err: fmt.Errorf("%w: %w", ErrModDeleteFailed, cleanupErr.Err)}
		}
		return fmt.Errorf("%w: %w", ErrModDeleteFailed, err)
	}
	return nil
}

func (m *manager) RemoveAllMods() error {
	return m.removeAllMods(ModRemoval.Commit)
}

func (m *manager) removeAllMods(commit func(ModRemoval) error) error {
	removal, err := m.StageAllModsRemoval()
	if err != nil {
		return err
	}
	if err := commit(removal); err != nil {
		if cleanupErr, completed := err.(*ModRemovalCleanupError); completed {
			return &ModRemovalCleanupError{Err: fmt.Errorf("%w: %w", ErrDeleteAllModsFailed, cleanupErr.Err)}
		}
		return fmt.Errorf("%w: %w", ErrDeleteAllModsFailed, err)
	}
	return nil
}

func (m *manager) StageModRemoval(fullName string) (ModRemoval, error) {
	target := filepath.Join(m.modDirectory, fullName)
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		return &modRemoval{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrModDeleteFailed, err)
	}
	return m.stageModRemoval(target, false, ErrModDeleteFailed)
}

func (m *manager) StageAllModsRemoval() (ModRemoval, error) {
	if err := os.MkdirAll(m.modDirectory, os.ModePerm); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDirectoryCreateFailed, err)
	}
	return m.stageModRemoval(m.modDirectory, true, ErrDeleteAllModsFailed)
}
