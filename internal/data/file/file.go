package file

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	ZipFileExtension = ".zip"
)

var (
	ErrFileOpenFailed   = errors.New("unable to open file")
	ErrFileCreateFailed = errors.New("unable to create file")
	ErrFileWriteFailed  = errors.New("unable to write data to file")
	ErrFileRenameFailed = errors.New("unable to rename file")
	ErrFileCopyFailed   = errors.New("unable to copy")

	ErrDirectoryCreateFailed = errors.New("unable to create new directory")
	ErrDirectoryOpenFailed   = errors.New("unable to open directory")
	ErrZipReadFailed         = errors.New("unable to read zip archive")
)

// Unzip is a helper function that takes a path to a zip file (source) and extracts all of its
// contents into a destination folder.
func Unzip(source, destination string) error {
	// Create the destination directory for all files
	err := os.MkdirAll(destination, os.ModePerm)
	if err != nil {
		return ErrDirectoryCreateFailed
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
				return ErrDirectoryCreateFailed
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

// createFile is a helper function that creates a new file and writes data from io.Reader into it
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

// moveFiles is a helper function for moving all files within a directory to another one
func moveFiles(source, destination string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return ErrDirectoryOpenFailed
	}

	for _, e := range entries {
		src := filepath.Join(source, e.Name())
		dest := filepath.Join(destination, e.Name())

		if err := os.Rename(src, dest); err != nil {
			return ErrFileRenameFailed
		}
	}
	return nil
}

func copyDirectory(source, destination string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if err := copyFile(path, target); err != nil {
			return fmt.Errorf("copy %q: %w", path, err)
		}
		return nil
	})
}

func copyFile(source, destination string) error {
	return copyFileWithDestination(source, destination, func(path string) (io.WriteCloser, error) {
		return os.Create(path)
	})
}

func copyFileWithDestination(source, destination string, createDestination func(string) (io.WriteCloser, error)) (err error) {
	src, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFileOpenFailed, err)
	}
	defer src.Close()

	dst, err := createDestination(destination)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFileCreateFailed, err)
	}
	defer func() {
		if closeErr := dst.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("%w: close %q: %w", ErrFileCopyFailed, destination, closeErr))
		}
	}()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("%w: %w", ErrFileCopyFailed, err)
	}

	info, err := src.Stat()
	if err != nil {
		return err
	}
	return os.Chmod(destination, info.Mode())
}
