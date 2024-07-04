package file

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"warden/internal/api"

	"github.com/schollz/progressbar/v3"
)

// An interface for all framework/BepInEx related file operations
type frameworkManager interface {
	// Downloads BepInEx, installs it, and migrates any existing mods to the new
	// plugin folder
	InstallBepInEx(url, fullName string) (string, error)

	// Updates BepInEx while maintaining any existing mods
	UpdateBepInEx(url, fullName string) error

	// Removes all BepInEx files
	RemoveBepInEx() error
}

func (m *manager) InstallBepInEx(url, fullName string) (string, error) {
	// Get the data
	resp, err := m.client.Get(url)
	if err != nil {
		return "", api.ErrHTTPClient
	}
	defer resp.Body.Close()

	m.backup.Create(m.valheimDirectory)

	bar := progressbar.DefaultBytes(
		resp.ContentLength,
		"Downloading BepInEx...",
	)

	// Create the zip archive
	zipPath := filepath.Join(m.valheimDirectory, fullName+".zip")
	if err := createFile(zipPath, resp.Body, bar); err != nil {
		m.backup.Restore(m.valheimDirectory)
		return "", err
	}

	// Extract zip files into Valheim server folder
	if err := Unzip(zipPath, m.valheimDirectory); err != nil {
		m.backup.Restore(m.valheimDirectory)
		return "", err
	}

	// Remove zip file after finishing extraction
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
		fmt.Printf("%+v", err)
		return err
	}
	return os.RemoveAll(path)
}
