package file

import (
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/api"
	"io"
	"os"
	"path/filepath"
)

// An interface for all framework/BepInEx related file operations
type frameworkManager interface {
	// Downloads BepInEx, installs it, and migrates any existing mods to the new
	// plugin folder
	InstallBepInEx(url, fullName string) (string, error)

	// Updates BepInEx while preserving existing plugins and BepInEx/config files.
	UpdateBepInEx(url, fullName string) error

	// Removes all BepInEx files
	RemoveBepInEx() error

	// Identifies the plugin tree removed with BepInEx.
	BepInExPluginDirectory() string
}

// FrameworkMutationCleanupError reports cleanup failure after the requested
// framework file operation completes, not an intermediate step or rollback.
// File managers return it directly; a nested marker does not establish completion
// of the enclosing operation. Callers must not roll back a completed change.
type FrameworkMutationCleanupError struct {
	Err error
}

func (e *FrameworkMutationCleanupError) Error() string {
	return fmt.Sprintf("framework files changed; cleanup failed: %v", e.Err)
}

func (e *FrameworkMutationCleanupError) Unwrap() error {
	return e.Err
}

func (m *manager) InstallBepInEx(url, fullName string) (string, error) {
	if err := preflightRestore(m.valheimDirectory); err != nil {
		return "", fmt.Errorf("%w: %w", ErrFrameworkInstallFailed, err)
	}

	resp, err := m.client.Get(url)
	if err != nil {
		return "", fmt.Errorf("%w: %w", api.ErrHTTPClient, err)
	}
	defer resp.Body.Close()

	if err := m.backup.Create(m.valheimDirectory); err != nil {
		return "", fmt.Errorf("%w: %w", ErrFrameworkInstallFailed, err)
	}

	if err := m.installBepInExFiles(resp.Body, fullName); err != nil {
		return "", errors.Join(err, m.restoreBackup(m.valheimDirectory))
	}
	if err := m.backup.Remove(); err != nil {
		return "", &FrameworkMutationCleanupError{Err: fmt.Errorf("%w: %w", ErrFrameworkInstallFailed, err)}
	}
	return m.valheimDirectory, nil
}

// The caller owns backup creation, restoration, and cleanup.
func (m *manager) installBepInExFiles(body io.Reader, fullName string) error {
	zipPath := filepath.Join(m.valheimDirectory, fullName+".zip")
	if err := createFile(zipPath, body); err != nil {
		return err
	}

	if err := Unzip(zipPath, m.valheimDirectory); err != nil {
		return err
	}

	if err := os.Remove(zipPath); err != nil {
		return fmt.Errorf("%w: %w", ErrZipDeleteFailed, err)
	}

	if err := m.moveBepInExFiles(); err != nil {
		return fmt.Errorf("%w: %w", ErrFrameworkInstallFailed, err)
	}
	return nil
}

func (m *manager) UpdateBepInEx(url, fullName string) error {
	return m.updateBepInEx(url, fullName, copyDirectory, os.RemoveAll, os.RemoveAll)
}

func (m *manager) updateBepInEx(url, fullName string, copyPlugins func(string, string) error, removeStaging, removeFrameworkFiles func(string) error) error {
	if err := preflightRestore(m.valheimDirectory); err != nil {
		return fmt.Errorf("%w: %w", ErrFrameworkUpdateFailed, err)
	}
	server, err := filepath.Abs(m.valheimDirectory)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFrameworkUpdateFailed, err)
	}
	if server == filepath.Dir(server) {
		return fmt.Errorf("%w: cannot stage plugins outside filesystem root %q", ErrFrameworkUpdateFailed, server)
	}
	if err := m.backup.Create(m.valheimDirectory); err != nil {
		return fmt.Errorf("%w: %w", ErrFrameworkUpdateFailed, err)
	}
	server, err = filepath.EvalSymlinks(server)
	if err != nil {
		return fmt.Errorf("%w: %w; backup retained at %q", ErrFrameworkUpdateFailed, err, *m.backup.Path())
	}
	// A sibling survives both framework removal and whole-server restoration.
	tmp, err := os.MkdirTemp(filepath.Dir(server), ".warden-plugins-")
	if err != nil {
		return fmt.Errorf("%w: stage plugins: %w; backup retained at %q", ErrFrameworkUpdateFailed, err, *m.backup.Path())
	}

	if err := copyPlugins(m.modDirectory, tmp); err != nil {
		cause := fmt.Errorf("%w: stage plugins: %w; backup retained at %q", ErrFrameworkUpdateFailed, err, *m.backup.Path())
		if cleanupErr := removeStaging(tmp); cleanupErr != nil {
			return errors.Join(cause, fmt.Errorf("%w: remove plugin staging %q: %w", ErrBackupDeleteFailed, tmp, cleanupErr))
		}
		return cause
	}

	fail := func(cause error) error {
		restoreErr := m.restoreBackup(m.valheimDirectory)
		return fmt.Errorf("%w: %w; plugin snapshot retained at %q",
			ErrFrameworkUpdateFailed, errors.Join(cause, restoreErr), tmp)
	}
	if err := m.removeBepInExFiles(removeFrameworkFiles); err != nil {
		return fail(err)
	}
	resp, err := m.client.Get(url)
	if err != nil {
		return fail(fmt.Errorf("%w: %w", api.ErrHTTPClient, err))
	}
	defer resp.Body.Close()
	if err := m.installBepInExFiles(resp.Body, fullName); err != nil {
		return fail(err)
	}
	config := filepath.Join("BepInEx", "config")
	if err := preserveBepInExConfig(filepath.Join(*m.backup.Path(), config), filepath.Join(server, config), removeStaging); err != nil {
		return fail(fmt.Errorf("preserve BepInEx configuration: %w", err))
	}
	transferred, cleanupErr := copyStagedPlugins(tmp, m.modDirectory, copyPlugins, removeStaging)
	if !transferred {
		return fail(cleanupErr)
	}

	if err := removeStaging(tmp); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("%w: remove plugin snapshot %q: %w", ErrBackupDeleteFailed, tmp, err))
	}
	cleanupErr = errors.Join(cleanupErr, m.backup.Remove())
	if cleanupErr != nil {
		return &FrameworkMutationCleanupError{Err: fmt.Errorf("%w: %w", ErrFrameworkUpdateFailed, cleanupErr)}
	}
	return nil
}

func preserveBepInExConfig(source, destination string, removeStaging func(string) error) error {
	info, err := os.Stat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("configuration source %q is not a directory", source)
	}

	type directoryMode struct {
		path string
		mode os.FileMode
	}
	var directories []directoryMode
	err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			mode := info.Mode() | 0o700
			if err := os.MkdirAll(target, mode); err != nil {
				return err
			}
			if err := os.Chmod(target, mode); err != nil {
				return err
			}
			directories = append(directories, directoryMode{path: target, mode: info.Mode()})
			return nil
		}
		return copyBepInExConfigFile(path, target, copyFile, removeStaging)
	})
	if err != nil {
		return err
	}
	// Publish descendants before restoring restrictive configuration directory modes.
	for i := len(directories) - 1; i >= 0; i-- {
		directory := directories[i]
		if err := os.Chmod(directory.path, directory.mode); err != nil {
			return fmt.Errorf("%w: set configuration directory permissions %q: %w", ErrFileCopyFailed, directory.path, err)
		}
	}
	return nil
}

func copyBepInExConfigFile(source, destination string, copyConfig func(string, string) error, removeStaging func(string) error) (err error) {
	staging, err := os.MkdirTemp(filepath.Dir(destination), ".warden-config-")
	if err != nil {
		return fmt.Errorf("%w: stage configuration %q: %w", ErrDirectoryCreateFailed, destination, err)
	}
	defer func() {
		if cleanupErr := removeStaging(staging); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("%w: remove configuration staging %q: %w", ErrBackupDeleteFailed, staging, cleanupErr))
		}
	}()

	// A private new inode prevents existing readers of defaults from seeing user bytes.
	replacement := filepath.Join(staging, "replacement")
	if err := copyConfig(source, replacement); err != nil {
		return fmt.Errorf("copy configuration %q: %w", source, err)
	}
	if err := os.Rename(replacement, destination); err != nil {
		return fmt.Errorf("%w: publish configuration %q: %w", ErrFileRenameFailed, destination, err)
	}
	return nil
}

func copyStagedPlugins(source, destination string, copyPlugins func(string, string) error, removeStaging func(string) error) (transferred bool, err error) {
	if err := os.MkdirAll(destination, os.ModePerm); err != nil {
		return false, fmt.Errorf("%w: prepare plugin destination %q: %w", ErrDirectoryCreateFailed, destination, err)
	}
	local, err := os.MkdirTemp(destination, ".warden-plugins-")
	if err != nil {
		return false, fmt.Errorf("%w: prepare plugin transfer: %w", ErrDirectoryCreateFailed, err)
	}
	defer func() {
		if cleanupErr := removeStaging(local); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("%w: remove plugin transfer %q: %w", ErrBackupDeleteFailed, local, cleanupErr))
		}
	}()
	if err := copyPlugins(source, local); err != nil {
		return false, err
	}
	if err := moveFiles(local, destination); err != nil {
		return false, err
	}
	return true, nil
}

func (m *manager) RemoveBepInEx() error {
	return m.removeBepInEx(os.RemoveAll)
}

func (m *manager) BepInExPluginDirectory() string {
	return m.modDirectory
}

func (m *manager) removeBepInEx(removeFiles func(string) error) error {
	if err := preflightRestore(m.valheimDirectory); err != nil {
		return fmt.Errorf("%w: %w", ErrFrameworkDeleteFailed, err)
	}
	if err := m.backup.Create(m.valheimDirectory); err != nil {
		return fmt.Errorf("%w: %w", ErrFrameworkDeleteFailed, err)
	}
	if err := m.removeBepInExFiles(removeFiles); err != nil {
		return errors.Join(err, m.restoreBackup(m.valheimDirectory))
	}
	if err := m.backup.Remove(); err != nil {
		return &FrameworkMutationCleanupError{Err: fmt.Errorf("%w: %w", ErrFrameworkDeleteFailed, err)}
	}
	return nil
}

// The caller owns backup creation, restoration, and cleanup.
func (m *manager) removeBepInExFiles(removeFiles func(string) error) error {
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
		err := removeFiles(f)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: remove %q: %w", ErrFrameworkDeleteFailed, f, err)
		}
	}
	return nil
}

func (m *manager) moveBepInExFiles() error {
	path := filepath.Join(m.valheimDirectory, BepInExContentsDirectory)

	if err := moveFiles(path, m.valheimDirectory); err != nil {
		return err
	}
	return os.RemoveAll(path)
}
