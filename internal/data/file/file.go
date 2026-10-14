package file

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// Unzip extracts an archive within the resolved destination directory.
// Extraction can leave partial files on error; the destination tree must not
// be changed concurrently.
func Unzip(source, destination string) (err error) {
	if err := os.MkdirAll(destination, os.ModePerm); err != nil {
		return fmt.Errorf("%w: %w", ErrDirectoryCreateFailed, err)
	}
	root, err := filepath.EvalSymlinks(destination)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDirectoryOpenFailed, err)
	}
	if !filepath.IsAbs(root) {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrDirectoryOpenFailed, err)
		}
		// Getwd may preserve a symlinked PWD whose parent differs from the physical cwd.
		wd, err = filepath.EvalSymlinks(wd)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrDirectoryOpenFailed, err)
		}
		root = filepath.Join(wd, root)
	}

	archive, err := zip.OpenReader(source)
	// OpenReader can return an archive alongside zip.ErrInsecurePath.
	if archive != nil {
		defer func() {
			if closeErr := archive.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close zip archive %q: %w", source, closeErr))
			}
		}()
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrZipReadFailed, err)
	}

	return extractArchive(&archive.Reader, root)
}

func extractArchive(archive *zip.Reader, root string) error {
	for _, f := range archive.File {
		filePath, err := extractionPath(root, f.Name)
		if err != nil {
			return err
		}

		dir := filepath.Dir(filePath)
		if f.FileInfo().IsDir() {
			dir = filePath
		}
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("%w: %w", ErrDirectoryCreateFailed, err)
		}
		if f.FileInfo().IsDir() {
			continue
		}

		srcFile, err := f.Open()
		if err != nil {
			return fmt.Errorf("%w: zip entry %q: %w", ErrFileOpenFailed, f.Name, err)
		}
		writeErr := createFile(filePath, srcFile)
		closeErr := srcFile.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return fmt.Errorf("extract zip entry %q: %w", f.Name, err)
		}
	}
	return nil
}

func extractionPath(root, name string) (string, error) {
	name = filepath.FromSlash(name)
	if !filepath.IsLocal(name) {
		return "", fmt.Errorf("%w: %q", zip.ErrInsecurePath, name)
	}

	path := root
	for _, part := range strings.Split(filepath.Clean(name), string(filepath.Separator)) {
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("inspect zip path %q: %w", name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return "", fmt.Errorf("%w: resolve %q: %w", zip.ErrInsecurePath, name, err)
			}
			relative, err := filepath.Rel(root, resolved)
			if err != nil {
				return "", fmt.Errorf("%w: resolve %q: %w", zip.ErrInsecurePath, name, err)
			}
			if !filepath.IsLocal(relative) {
				return "", fmt.Errorf("%w: %q", zip.ErrInsecurePath, name)
			}
		} else if !info.IsDir() {
			break
		}
	}
	return filepath.Join(root, name), nil
}

func createFile(filePath string, fileSource io.Reader) (err error) {
	out, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFileCreateFailed, err)
	}
	defer func() {
		if closeErr := out.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("%w: close %q: %w", ErrFileWriteFailed, filePath, closeErr))
		}
	}()

	if _, err := io.Copy(out, fileSource); err != nil {
		return fmt.Errorf("%w: %w", ErrFileWriteFailed, err)
	}
	return nil
}

// moveFiles is a helper function for moving all files within a directory to another one
func moveFiles(source, destination string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDirectoryOpenFailed, err)
	}

	for _, e := range entries {
		src := filepath.Join(source, e.Name())
		dest := filepath.Join(destination, e.Name())

		if err := os.Rename(src, dest); err != nil {
			return fmt.Errorf("%w: %w", ErrFileRenameFailed, err)
		}
	}
	return nil
}

func copyDirectory(source, destination string) error {
	type directoryMode struct {
		path string
		mode os.FileMode
	}
	var directories []directoryMode
	err := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			if err := os.MkdirAll(target, info.Mode()|0o700); err != nil {
				return err
			}
			// Caller-owned staging roots retain their private permissions.
			if relative != "." {
				directories = append(directories, directoryMode{path: target, mode: info.Mode()})
			}
			return nil
		}
		if err := copyFile(path, target); err != nil {
			return fmt.Errorf("copy %q: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Populate descendants before restoring potentially restrictive modes.
	for i := len(directories) - 1; i >= 0; i-- {
		directory := directories[i]
		if err := os.Chmod(directory.path, directory.mode); err != nil {
			return fmt.Errorf("%w: set directory permissions %q: %w", ErrFileCopyFailed, directory.path, err)
		}
	}
	return nil
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
