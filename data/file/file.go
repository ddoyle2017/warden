package file

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
)

var (
	ErrFileOpenFailed   = errors.New("unable to open file")
	ErrFileCreateFailed = errors.New("unable to create file")
	ErrFileWriteFailed  = errors.New("unable to write data to file")

	ErrCreateDirectoryFailed = errors.New("unable to create new directory")
	ErrZipReadFailed         = errors.New("unable to read zip archive")
)

// Unzip() is a helper function that takes a path to a zip file (source) and extracts all of its
// contents into a destination folder.
func Unzip(source, destination string) error {
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
				return ErrCreateDirectoryFailed
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
	return nil
}

// createFile() is a helper function that creates a new file and writes data from io.Reader into it
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