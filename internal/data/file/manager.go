package file

import (
	"errors"
	"os"
	"path/filepath"
	"warden/internal/api"
)

const (
	// BepInEx is required by practically every mod for Valheim, so we use it
	// for the default path
	BepInExPluginDirectory = "/BepInEx/plugins"

	// A sub-directory containing the files and libraries needed for BepInEx to work
	BepInExContentsDirectory = "/BepInExPack_Valheim"
)

var (
	ErrZipDeleteFailed        = errors.New("unable to delete zip archive")
	ErrModDeleteFailed        = errors.New("unable to delete mod directory")
	ErrDeleteAllModsFailed    = errors.New("unable to delete all mods")
	ErrFrameworkInstallFailed = errors.New("unable to install framework")
	ErrFrameworkDeleteFailed  = errors.New("unable to delete framework")
	ErrFrameworkUpdateFailed  = errors.New("unable to update framework")
)

// Manager provides an interface for all file-related mod operations, e.g. installing and deleting mods.
type Manager interface {
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

	// Downloads BepInEx, installs it, and migrates any existing mods to the new
	// plugin folder
	InstallBepInEx(url, fullName string) (string, error)

	// Updates BepInEx while maintaining any existing mods
	UpdateBepInEx(url, fullName string) error

	// Removes all BepInEx files
	RemoveBepInEx() error
}

type manager struct {
	backup           Backup
	client           api.HTTPClient
	valheimDirectory string
	modDirectory     string
}

func NewManager(c api.HTTPClient, vd string) Manager {
	return &manager{
		backup:           NewBackup(),
		client:           c,
		valheimDirectory: vd,
		modDirectory:     filepath.Join(vd, BepInExPluginDirectory),
	}
}

func (m *manager) InstallMod(url, fullName string) (string, error) {
	// Get the data
	resp, err := m.client.Get(url)
	if err != nil {
		return "", api.ErrHTTPClient
	}
	defer resp.Body.Close()

	m.backup.Create(m.modDirectory)

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

func (m *manager) InstallBepInEx(url, fullName string) (string, error) {
	// Get the data
	resp, err := m.client.Get(url)
	if err != nil {
		return "", api.ErrHTTPClient
	}
	defer resp.Body.Close()

	m.backup.Create(m.valheimDirectory)

	// Create the zip archive
	zipPath := filepath.Join(m.valheimDirectory, fullName+".zip")
	if err := createFile(zipPath, resp.Body); err != nil {
		m.backup.Restore(m.valheimDirectory)
		return "", err
	}

	// Extract zip files into Valheim server folder
	if err := Unzip(zipPath, m.valheimDirectory); err != nil {
		m.backup.Restore(m.valheimDirectory)
		return "", err
	}

	// Remove zip file after finishing extractio
	if err = os.Remove(zipPath); err != nil {
		m.backup.Restore(m.valheimDirectory)
		return "", ErrZipDeleteFailed
	}

	// Move BepInEx files to Valheim installation directory and remove top level folder
	if err := m.moveBepInExFiles(); err != nil {
		m.backup.Restore(m.valheimDirectory)
		return "", ErrFrameworkInstallFailed
	}
	m.backup.Remove()
	return m.valheimDirectory, nil
}

func (m *manager) UpdateBepInEx(url, fullName string) error {
	m.backup.Create(m.valheimDirectory)

	// Move BepInEx mods to /tmp
	tmp, err := os.MkdirTemp("", "warden")
	if err != nil {
		m.backup.Restore(m.valheimDirectory)
		return ErrFrameworkUpdateFailed
	}
	defer os.RemoveAll(tmp)

	if err := moveFiles(m.modDirectory, tmp); err != nil {
		m.backup.Restore(m.valheimDirectory)
		return ErrFrameworkUpdateFailed
	}

	// Update BepInEx
	if err := m.RemoveBepInEx(); err != nil {
		m.backup.Restore(m.valheimDirectory)
		return ErrFrameworkUpdateFailed
	}
	if _, err := m.InstallBepInEx(url, fullName); err != nil {
		m.backup.Restore(m.valheimDirectory)
		return ErrFrameworkUpdateFailed
	}

	// Move mods back to BepInEx mods folder
	if err := moveFiles(tmp, m.modDirectory); err != nil {
		m.backup.Restore(m.valheimDirectory)
		return ErrFrameworkUpdateFailed
	}
	m.backup.Remove()
	return nil
}

func (m *manager) RemoveBepInEx() error {
	files := []string{
		filepath.Join(m.valheimDirectory, "BepInEx"),                 // core BepInEx files
		filepath.Join(m.valheimDirectory, "doorstop_libs"),           // dynamic libraries
		filepath.Join(m.valheimDirectory, "doorstop_config.ini"),     // dynamic library config
		filepath.Join(m.valheimDirectory, "icon.png"),                // BepInEx icon
		filepath.Join(m.valheimDirectory, "manifest.json"),           // BepInEx metadata
		filepath.Join(m.valheimDirectory, "README.md"),               // BepInEx README
		filepath.Join(m.valheimDirectory, "winhttp.dll"),             // Windows HTTP service DLL that BepInEx includes
		filepath.Join(m.valheimDirectory, "start_game_bepinex.sh"),   // BepInEx script for starting game client
		filepath.Join(m.valheimDirectory, "start_server_bepinex.sh"), // BepInEx script for starting game server
		filepath.Join(m.valheimDirectory, "CHANGELOG.md"),            // BepInEx markdown change log
		filepath.Join(m.valheimDirectory, "changelog.txt"),           // BepInEx plain text change log
	}
	m.backup.Create(m.valheimDirectory)

	for _, f := range files {
		err := os.RemoveAll(f)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			m.backup.Restore(m.valheimDirectory)
			return ErrFrameworkDeleteFailed
		}
	}
	m.backup.Remove()
	return nil
}

func (m *manager) moveBepInExFiles() error {
	path := filepath.Join(m.valheimDirectory, BepInExContentsDirectory)

	if err := moveFiles(path, m.valheimDirectory); err != nil {
		return err
	}
	return os.RemoveAll(path)
}
