package file

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPreserveBepInExConfigRetainedReader(t *testing.T) {
	for name, mode := range map[string]os.FileMode{
		"given private settings and an open default, when preserving, then keep private bytes out of the old reader":   0o600,
		"given shared settings and an open default, when preserving, then retain shared mode without reusing the file": 0o640,
		"given read-only settings and an open default, when preserving, then retain their bytes and mode":              0o400,
	} {
		t.Run(name, func(t *testing.T) {
			source, destination := t.TempDir(), t.TempDir()
			saved := map[string]string{"BepInEx.cfg": "original settings\x00\xff"}
			writeRestoreTree(t, source, saved)
			writeRestoreTree(t, destination, map[string]string{
				"BepInEx.cfg": "packaged defaults",
				"new.cfg":     "new settings",
			})
			if err := os.Chmod(filepath.Join(source, "BepInEx.cfg"), mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(source, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(source, "empty"), 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(destination, "BepInEx.cfg")
			if err := os.Chmod(target, 0o644); err != nil {
				t.Fatal(err)
			}
			reader, err := os.Open(target)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()

			if err := preserveBepInExConfig(source, destination); err != nil {
				t.Fatal(err)
			}
			if data, err := io.ReadAll(reader); err != nil || string(data) != "packaged defaults" {
				t.Errorf("reader of the public default received replacement bytes: %q, %v", data, err)
			}
			assertRestoreTree(t, source, saved)
			assertRestoreTree(t, destination, map[string]string{
				"BepInEx.cfg": "original settings\x00\xff",
				"new.cfg":     "new settings",
			})
			if info, err := os.Stat(target); err != nil || info.Mode().Perm() != mode {
				t.Errorf("configuration mode changed: %v, %v; want %o", info, err, mode)
			}
			for path, want := range map[string]os.FileMode{destination: 0o750, filepath.Join(destination, "empty"): 0o700} {
				if info, err := os.Stat(path); err != nil || !info.IsDir() || info.Mode().Perm() != want {
					t.Errorf("configuration directory mode changed at %q: %v, %v", path, info, err)
				}
			}
		})
	}
}

func TestCopyBepInExConfigFilePublication(t *testing.T) {
	tests := map[string]struct {
		existing     bool
		interrupt    bool
		closeFailure bool
		mode         os.FileMode
	}{
		"given a private configuration and public default, when copying, then publish only the completed private replacement": {
			existing: true, mode: 0o600,
		},
		"given a shared configuration, when copying, then restrict staging until publication and preserve the shared mode": {
			existing: true, mode: 0o640,
		},
		"given no packaged default, when copying, then leave the public path absent until publication": {
			mode: 0o600,
		},
		"given a public default, when copying is interrupted, then keep default bytes and remove the private partial copy": {
			existing: true, interrupt: true, mode: 0o600,
		},
		"given no packaged default, when copying is interrupted, then publish no partial file": {
			interrupt: true, mode: 0o600,
		},
		"given complete bytes and a destination close failure, when returning, then retain the default and discard the replacement": {
			existing: true, closeFailure: true, mode: 0o600,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sourceRoot, destination := t.TempDir(), t.TempDir()
			source := filepath.Join(sourceRoot, "user.cfg")
			target := filepath.Join(destination, "user.cfg")
			saved := map[string]string{"user.cfg": "private configuration bytes"}
			defaults := map[string]string{}
			if tt.existing {
				defaults["user.cfg"] = "packaged defaults"
			}
			writeRestoreTree(t, sourceRoot, saved)
			writeRestoreTree(t, destination, defaults)
			if err := os.Chmod(source, tt.mode); err != nil {
				t.Fatal(err)
			}
			if tt.existing {
				if err := os.Chmod(target, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			fault := errors.New("configuration copy failed")
			var staging string
			copies, closes := 0, 0
			err := copyBepInExConfigFile(source, target, func(from, prepared string) error {
				copies++
				staging = filepath.Dir(prepared)
				if from != source || prepared == target || filepath.Dir(staging) != destination {
					t.Fatalf("copy is not prepared separately beside the destination: %q -> %q", from, prepared)
				}
				checkBoundary := func() {
					t.Helper()
					if info, err := os.Stat(staging); err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
						t.Fatalf("copy staging is accessible to other users: %v, %v", info, err)
					}
					data, err := os.ReadFile(target)
					if tt.existing {
						if err != nil || string(data) != "packaged defaults" {
							t.Errorf("default changed before publication: %q, %v", data, err)
						}
					} else if !errors.Is(err, os.ErrNotExist) {
						t.Errorf("new configuration was visible before publication: %q, %v", data, err)
					}
				}
				checkBoundary()
				if tt.interrupt {
					if err := os.WriteFile(prepared, []byte("partial private bytes"), 0o644); err != nil {
						t.Fatal(err)
					}
					checkBoundary()
					return fmt.Errorf("%w: %w", ErrFileCopyFailed, fault)
				}
				if tt.closeFailure {
					err := copyFileWithDestination(from, prepared, func(path string) (io.WriteCloser, error) {
						dst, err := os.Create(path)
						if err != nil {
							return nil, err
						}
						return &completionFailureFile{file: dst, closeErr: fault, closes: &closes}, nil
					})
					checkBoundary()
					return err
				}
				if err := copyFile(from, prepared); err != nil {
					return err
				}
				checkBoundary()
				return nil
			})
			if tt.interrupt || tt.closeFailure {
				if !errors.Is(err, ErrFileCopyFailed) || !errors.Is(err, fault) {
					t.Fatalf("copy failure lost its cause: %v", err)
				}
				assertRestoreTree(t, destination, defaults)
			} else {
				if err != nil {
					t.Fatal(err)
				}
				assertRestoreTree(t, destination, saved)
				if info, err := os.Stat(target); err != nil || info.Mode().Perm() != tt.mode {
					t.Errorf("published mode changed: %v, %v; want %o", info, err, tt.mode)
				}
			}
			assertRestoreTree(t, sourceRoot, saved)
			if copies != 1 {
				t.Errorf("expected one configuration copy, got %d", copies)
			}
			if tt.closeFailure && closes != 1 {
				t.Errorf("destination close calls = %d, want 1", closes)
			}
			if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("configuration staging was not removed: %v", err)
			}
		})
	}
}

func TestPreserveBepInExConfigDirectoryModes(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	modes := map[string]os.FileMode{".": 0o750, "nested": 0o555, "empty": 0o500}
	t.Cleanup(func() {
		for _, root := range []string{source, destination} {
			for _, name := range []string{".", "nested", "empty"} {
				if err := os.Chmod(filepath.Join(root, name), 0o700); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Errorf("restore fixture cleanup access: %v", err)
				}
			}
		}
	})
	saved := map[string]string{"nested/user.cfg": "user settings"}
	writeRestoreTree(t, source, saved)
	writeRestoreTree(t, destination, map[string]string{"nested/new.cfg": "new defaults"})
	if err := os.Mkdir(filepath.Join(source, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, mode := range modes {
		if err := os.Chmod(filepath.Join(source, name), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := preserveBepInExConfig(source, destination); err != nil {
		t.Fatal(err)
	}
	assertRestoreTree(t, source, saved)
	assertRestoreTree(t, destination, map[string]string{
		"nested/user.cfg": "user settings",
		"nested/new.cfg":  "new defaults",
	})
	for name, mode := range modes {
		info, err := os.Stat(filepath.Join(destination, name))
		if err != nil || !info.IsDir() || info.Mode().Perm() != mode {
			t.Errorf("configuration directory %q = %v, %v; want %o", name, info, err, mode)
		}
	}
}
