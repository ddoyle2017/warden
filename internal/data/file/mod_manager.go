package file

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"warden/internal/api"

	"github.com/schollz/progressbar/v3"
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
}

func (m *manager) InstallMod(url, fullName string) (string, error) {
	// Get the data
	resp, err := m.client.Get(url)
	if err != nil {
		return "", api.ErrHTTPClient
	}
	defer resp.Body.Close()

	m.backup.Create(m.modDirectory)

	bar := progressbar.DefaultBytes(
		resp.ContentLength,
		fmt.Sprintf("Downloading %s...", fullName),
	)

	// Create the zip archive
	zipPath := filepath.Join(m.modDirectory, fullName+".zip")
	if err := createFile(zipPath, resp.Body, bar); err != nil {
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
	modPath := filepath.Join(m.modDirectory, fullName)

	m.backup.Create(m.modDirectory)
	err := os.RemoveAll(modPath)

	// If error is thrown because the file does not exist, we ignore. For
	// any other error, return that the delete failed.
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		m.backup.Restore(m.modDirectory)
		return ErrModDeleteFailed
	}
	m.backup.Remove()
	return nil
}

func (m *manager) RemoveAllMods() error {
	m.backup.Create(m.modDirectory)

	// Delete the parent folder for all mods and everything inside
	err := os.RemoveAll(m.modDirectory)
	if err != nil {
		m.backup.Restore(m.modDirectory)
		return ErrDeleteAllModsFailed
	}

	// Recreate parent folder for all mods
	err = os.MkdirAll(m.modDirectory, os.ModePerm)
	if err != nil {
		m.backup.Restore(m.modDirectory)
		return ErrDirectoryCreateFailed
	}
	m.backup.Remove()
	return nil
}
