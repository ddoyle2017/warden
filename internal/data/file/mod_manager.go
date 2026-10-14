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
	// Downloads the targetted mod, unzips it, and adds it to the mod
	// folder.
	//
	// URL is the download link for a specific release.
	// FullName is the namespace + mod name + version string that Thunderstore provides.
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

func (m *manager) InstallMod(url, fullName string) (string, error) {
	if err := preflightRestore(m.modDirectory); err != nil {
		return "", err
	}

	// Get the data
	resp, err := m.client.Get(url)
	if err != nil {
		return "", api.ErrHTTPClient
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
		m.backup.Restore(m.modDirectory)
		return "", err
	}

	// Extract zip files into a new folder for the mod
	destination := filepath.Join(m.modDirectory, fullName)
	if err := Unzip(zipPath, destination); err != nil {
		m.backup.Restore(m.modDirectory)
		return "", err
	}

	// Remove zip file after finishing extraction
	if err := os.Remove(zipPath); err != nil {
		m.backup.Restore(m.modDirectory)
		return "", ErrZipDeleteFailed
	}
	m.backup.Remove()
	return destination, nil
}

func (m *manager) RemoveMod(fullName string) error {
	removal, err := m.StageModRemoval(fullName)
	if err != nil {
		return err
	}
	if err := removal.Commit(); err != nil {
		return fmt.Errorf("%w: %w", ErrModDeleteFailed, err)
	}
	return nil
}

func (m *manager) RemoveAllMods() error {
	removal, err := m.StageAllModsRemoval()
	if err != nil {
		return err
	}
	if err := removal.Commit(); err != nil {
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
