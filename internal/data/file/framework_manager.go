package file

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"warden/internal/api"
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

// FrameworkMutationCleanupError reports cleanup failure after the requested
// framework file operation completes, not an intermediate step or rollback.
// Callers must not roll back that change.
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
		return "", errors.Join(err, m.restoreServerBackup())
	}
	if err := m.backup.Remove(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrFrameworkInstallFailed, &FrameworkMutationCleanupError{Err: err})
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
		restoreErr := m.restoreServerBackup()
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
	transferred, cleanupErr := copyStagedPlugins(tmp, m.modDirectory, copyPlugins, removeStaging)
	if !transferred {
		return fail(cleanupErr)
	}

	if err := removeStaging(tmp); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("%w: remove plugin snapshot %q: %w", ErrBackupDeleteFailed, tmp, err))
	}
	cleanupErr = errors.Join(cleanupErr, m.backup.Remove())
	if cleanupErr != nil {
		return fmt.Errorf("%w: %w", ErrFrameworkUpdateFailed, &FrameworkMutationCleanupError{Err: cleanupErr})
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

func (m *manager) removeBepInEx(removeFiles func(string) error) error {
	if err := preflightRestore(m.valheimDirectory); err != nil {
		return fmt.Errorf("%w: %w", ErrFrameworkDeleteFailed, err)
	}
	if err := m.backup.Create(m.valheimDirectory); err != nil {
		return fmt.Errorf("%w: %w", ErrFrameworkDeleteFailed, err)
	}
	if err := m.removeBepInExFiles(removeFiles); err != nil {
		return errors.Join(err, m.restoreServerBackup())
	}
	if err := m.backup.Remove(); err != nil {
		return fmt.Errorf("%w: %w", ErrFrameworkDeleteFailed, &FrameworkMutationCleanupError{Err: err})
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

func (m *manager) restoreServerBackup() error {
	err := m.backup.Restore(m.valheimDirectory)
	if path := m.backup.Path(); err != nil && path != nil {
		return fmt.Errorf("%w; backup retained at %q", err, *path)
	}
	return err
}

func (m *manager) moveBepInExFiles() error {
	path := filepath.Join(m.valheimDirectory, BepInExContentsDirectory)

	if err := moveFiles(path, m.valheimDirectory); err != nil {
		return err
	}
	return os.RemoveAll(path)
}
