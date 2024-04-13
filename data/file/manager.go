package file

import (
	"errors"
	"os"
	"path/filepath"
	"warden/api"
)

var (
	ErrZipDeleteFailed     = errors.New("unable to delete zip archive")
	ErrModDeleteFailed     = errors.New("unable to delete mod directory")
	ErrDeleteAllModsFailed = errors.New("unable to delete all mods")
)

// Manager provides an interface for all file-related mod operations, e.g. installing and deleting mods.
type Manager interface {
	// InstallMod() downloads the targetted mod, unzips it, and adds it to the mod
	// folder.
	//
	// URL is the download link for a specific release.
	// FullName is the namespace + mod name + version string that Thunderstore provides.
	InstallMod(url, fullName string) (string, error)

	// RemoveMod() deletes the folder and contents for a mod. `FullName` is a
	// value provided by Thunderstore that contains the name, namespace, and version of a
	// specific mod release.
	RemoveMod(fullName string) error

	// RemoveAllMods deletes the parent mod folder and all of its contents, then recreates an empty one.
	RemoveAllMods() error
}

type manager struct {
	client    api.HTTPClient
	modFolder string
}

func NewManager(mf string, c api.HTTPClient) Manager {
	return &manager{
		modFolder: mf,
		client:    c,
	}
}

func (m *manager) InstallMod(url, fullName string) (string, error) {
	// Get the data
	resp, err := m.client.Get(url)
	if err != nil {
		return "", api.ErrHTTPClient
	}
	defer resp.Body.Close()

	// Create the zip archive
	zipPath := filepath.Join(m.modFolder, fullName+".zip")

	err = createFile(zipPath, resp.Body)
	if err != nil {
		return "", err
	}

	// Extract zip files into a new folder for the mod
	destination := filepath.Join(m.modFolder, fullName)
	err = Unzip(zipPath, destination)
	if err != nil {
		return "", err
	}

	// Remove zip file after finishing extraction
	err = os.Remove(zipPath)
	if err != nil {
		return "", ErrZipDeleteFailed
	}
	return destination, nil
}

func (m *manager) RemoveMod(fullName string) error {
	modPath := filepath.Join(m.modFolder, fullName)

	err := os.RemoveAll(modPath)

	// If error is thrown because the file does not exist, we ignore. For
	// any other error, return that the delete failed.
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrModDeleteFailed
	}
	return nil
}

func (m *manager) RemoveAllMods() error {
	// Delete the parent folder for all mods and everything inside
	err := os.RemoveAll(m.modFolder)
	if err != nil {
		return ErrDeleteAllModsFailed
	}

	// Recreate parent folder for all mods
	err = os.MkdirAll(m.modFolder, os.ModePerm)
	if err != nil {
		return ErrCreateDirectoryFailed
	}
	return nil
}
