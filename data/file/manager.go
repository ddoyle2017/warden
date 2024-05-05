package file

import (
	"errors"
	"os"
	"path/filepath"
	"warden/api"
)

const (
	// BepInEx is required by practically every mod for Valheim, so we use it
	// for the default path
	BepInExPluginDirectory = "/BepInEx/plugins"

	// A sub-directory containing the files and libraries needed for BepInEx to work
	BepInExContentsDirectory = "/BepInExPack_Valheim"
)

var (
	ErrZipDeleteFailed       = errors.New("unable to delete zip archive")
	ErrModDeleteFailed       = errors.New("unable to delete mod directory")
	ErrDeleteAllModsFailed   = errors.New("unable to delete all mods")
	ErrFrameworkDeleteFailed = errors.New("unable to delete framework")
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

	// Removes all BepInEx files
	RemoveBepInEx() error
}

type manager struct {
	client           api.HTTPClient
	valheimDirectory string
	modDirectory     string
}

func NewManager(c api.HTTPClient, sf string) Manager {
	return &manager{
		client:           c,
		valheimDirectory: sf,
		modDirectory:     filepath.Join(sf, BepInExPluginDirectory),
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
	zipPath := filepath.Join(m.modDirectory, fullName+".zip")

	err = createFile(zipPath, resp.Body)
	if err != nil {
		return "", err
	}

	// Extract zip files into a new folder for the mod
	destination := filepath.Join(m.modDirectory, fullName)
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

func (m *manager) InstallBepInEx(url, fullName string) (string, error) {
	// Get the data
	resp, err := m.client.Get(url)
	if err != nil {
		return "", api.ErrHTTPClient
	}
	defer resp.Body.Close()

	// Create the zip archive
	zipPath := filepath.Join(m.valheimDirectory, fullName+".zip")

	err = createFile(zipPath, resp.Body)
	if err != nil {
		return "", err
	}

	// Extract zip files into Valheim server folder
	err = Unzip(zipPath, m.valheimDirectory)
	if err != nil {
		return "", err
	}

	// Remove zip file after finishing extraction
	err = os.Remove(zipPath)
	if err != nil {
		return "", ErrZipDeleteFailed
	}

	// Move BepInEx files to Valheim installation directory and remove top level folder
	m.moveBepInExFiles()
	return m.valheimDirectory, nil
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

	for _, f := range files {
		err := os.RemoveAll(f)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrFrameworkDeleteFailed
		}
	}
	return nil
}

func (m *manager) RemoveMod(fullName string) error {
	modPath := filepath.Join(m.modDirectory, fullName)

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
	err := os.RemoveAll(m.modDirectory)
	if err != nil {
		return ErrDeleteAllModsFailed
	}

	// Recreate parent folder for all mods
	err = os.MkdirAll(m.modDirectory, os.ModePerm)
	if err != nil {
		return ErrCreateDirectoryFailed
	}
	return nil
}

func (m *manager) moveBepInExFiles() {
	path := filepath.Join(m.valheimDirectory, BepInExContentsDirectory)

	entries, err := os.ReadDir(path)
	if err != nil {
		panic("BING BONG")
	}
	for _, e := range entries {
		source := filepath.Join(path, e.Name())
		dest := filepath.Join(m.valheimDirectory, e.Name())
		if err := os.Rename(source, dest); err != nil {
			panic("BING BONG")
		}
	}
	if err := os.RemoveAll(path); err != nil {
		panic("BING BONG")
	}
}
