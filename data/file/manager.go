package file

import (
	"archive/zip"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"warden/api"
)

var (
	ErrFileOpenFailed   = errors.New("unable to open file")
	ErrFileCreateFailed = errors.New("unable to create file")
	ErrFileWriteFailed  = errors.New("unable to write data to file")

	ErrZipReadFailed   = errors.New("unable to read zip archive")
	ErrZipDeleteFailed = errors.New("unable to delete zip archive")

	ErrModDeleteFailed       = errors.New("unable to delete mod directory")
	ErrDeleteAllModsFailed   = errors.New("unable to delete all mods")
	ErrCreateDirectoryFailed = errors.New("unable to create new directory")
)

// Manager provides an interface for all file-related mod operations, e.g. installing and deleting mods.
type Manager interface {
	// InstallMod() downloads the targetted mod, unzips it, and adds it to the mod
	// folder.
	//
	// URL is the download link for a specific release.
	// FullName is the namespace + mod name + version string that Thunderstore provides.
	InstallMod(url, fullName string) error

	// RemoveMod() deletes the folder and contents for a mod. `FullName` is a
	// value provided by Thunderstore that contains the name, namespace, and version of a
	// specific mod release.
	RemoveMod(fullName string) error

	// RemoveAllMods deletes the parent mod folder and all of its contents, then recreates an empty one.
	RemoveAllMods() error
}

type manager struct {
	client    *http.Client
	modFolder string
}

func NewManager(mf string, c *http.Client) Manager {
	return &manager{
		modFolder: mf,
		client:    c,
	}
}

func (m *manager) InstallMod(url, fullName string) error {
	// Get the data
	resp, err := m.client.Get(url)
	if err != nil {
		return api.ErrHTTPClient
	}
	defer resp.Body.Close()

	// Create the zip archive
	zipPath := filepath.Join(m.modFolder, fullName+".zip")

	err = createFile(zipPath, resp.Body)
	if err != nil {
		return err
	}

	destination := filepath.Join(m.modFolder, fullName)
	return unzip(zipPath, destination)
}

func (m *manager) RemoveMod(fullName string) error {
	modPath := filepath.Join(m.modFolder, fullName)

	err := os.RemoveAll(modPath)
	if err != nil {
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

// unzip() is a helper function that takes a path to a zip folder (source) and extracts all of its
// contents into a destination folder.
func unzip(source, destination string) error {
	// Create the destination directory for all mod files
	err := os.MkdirAll(destination, os.ModePerm)
	if err != nil {
		return ErrCreateDirectoryFailed
	}

	// Open zip archive for reading
	archive, err := zip.OpenReader(source)
	if err != nil {
		return ErrZipReadFailed
	}
	defer archive.Close()

	// Loop through each file inside of the zip
	for _, f := range archive.File {
		filePath := filepath.Join(destination, f.Name)

		// Check if the file is a directory and create one if it is
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(filePath, os.ModePerm); err != nil {
				panic(err)
			}
			continue
		}

		// Open the file in the zip and copy its contents to the destination file
		srcFile, err := f.Open()
		if err != nil {
			return ErrFileOpenFailed
		}
		defer srcFile.Close()

		createFile(filePath, srcFile)
	}
	// Remove zip file after finishing extraction
	err = os.Remove(source)
	if err != nil {
		return ErrZipDeleteFailed
	}
	return nil
}

func createFile(filePath string, fileSource io.Reader) error {
	// Create the empty file
	out, err := os.Create(filePath)
	if err != nil {
		return ErrFileCreateFailed
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, fileSource)
	if err != nil {
		return ErrFileWriteFailed
	}
	return nil
}
