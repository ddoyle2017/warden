package file

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestModReplacementPublicationFailures(t *testing.T) {
	for _, phase := range []string{"archive deletion", "retain original", "publish", "recover", "recovery conflict", "restored cleanup", "preparation cleanup"} {
		t.Run("given a prepared replacement, when "+phase+" fails, then preserve originals and report the actual recovery outcome", func(t *testing.T) {
			server := filepath.Join(restoreTempDir(t), "server")
			m := NewManager(&pluginHTTPClient{GetFunc: func(string) (*http.Response, error) {
				return &http.Response{Body: io.NopCloser(bytes.NewReader(modRecoveryArchive(t, false)))}, nil
			}}, server).(*manager)
			oldPath := filepath.Join(m.modDirectory, "Owner-Plugin-1.0.0")
			newPath := filepath.Join(m.modDirectory, "Owner-Plugin-2.0.0")
			original := map[string]string{"plugin.dll": "working DLL", "nested/settings": "original settings"}
			writeRestoreTree(t, oldPath, original)
			writeRestoreTree(t, m.modDirectory, map[string]string{"unrelated.dll": "unrelated DLL"})
			if err := os.Chmod(oldPath, 0o750); err != nil {
				t.Fatal(err)
			}
			primary, recoveryCause, cleanupCause := errors.New("replacement interrupted"), errors.New("recovery interrupted"), errors.New("cleanup interrupted")
			var staging, originalPath, preparedPath string
			var moved, restored, cleaned bool
			handle, err := m.replaceMod("fixture", "Owner-Plugin-1.0.0", "Owner-Plugin-2.0.0", modReplacementOperations{
				removeArchive: func(path string) error {
					staging = filepath.Dir(path)
					if filepath.Dir(staging) != filepath.Dir(m.modDirectory) {
						t.Fatalf("staging is not outside plugins: %q", staging)
					}
					if info, err := os.Stat(staging); err != nil || info.Mode().Perm() != 0o700 {
						t.Fatalf("staging is not private: %v, %v", info, err)
					}
					assertRestoreTree(t, oldPath, original)
					assertRestoreTree(t, filepath.Join(staging, "replacement"), map[string]string{"plugin.dll": "new plugin"})
					if phase == "archive deletion" || phase == "preparation cleanup" {
						return primary
					}
					return os.Remove(path)
				},
				rename: func(source, destination string) error {
					switch {
					case source == oldPath:
						if phase == "archive deletion" || phase == "preparation cleanup" {
							t.Fatal("preparation failure reached destructive publication")
						}
						originalPath = destination
						if phase == "retain original" {
							return errors.Join(primary, syscall.EXDEV)
						}
						moved = true
						return os.Rename(source, destination)
					case destination == newPath:
						preparedPath = source
						assertRestoreTree(t, originalPath, original)
						if phase == "recovery conflict" {
							writeRestoreTree(t, oldPath, map[string]string{"concurrent.dll": "do not overwrite"})
						}
						return primary
					case destination == oldPath:
						if phase == "recover" {
							return recoveryCause
						}
						restored = true
						return os.Rename(source, destination)
					default:
						t.Fatalf("unexpected rename %q -> %q", source, destination)
						return nil
					}
				},
				removeAll: func(path string) error {
					cleaned = true
					if path != staging {
						t.Fatalf("cleanup targeted unowned path %q", path)
					}
					if phase == "restored cleanup" || phase == "preparation cleanup" {
						return cleanupCause
					}
					return os.RemoveAll(path)
				},
			})
			if handle != nil || !errors.Is(err, primary) {
				t.Fatalf("incomplete replacement returned %v, %v", handle, err)
			}
			if _, completed := err.(*ModInstallCleanupError); completed {
				t.Fatalf("incomplete publication claimed completion: %v", err)
			}
			recoveryFailed := phase == "recover" || phase == "recovery conflict"
			if errors.Is(err, ErrBackupRestoreFailed) != recoveryFailed {
				t.Errorf("actual recovery outcome misclassified: %v", err)
			}
			if recoveryFailed {
				if cleaned || restored || !moved || !strings.Contains(err.Error(), originalPath) || !strings.Contains(err.Error(), preparedPath) {
					t.Fatalf("failed recovery consumed or hid its retained trees: %v", err)
				}
				if phase == "recover" && !errors.Is(err, recoveryCause) {
					t.Errorf("lost recovery failure: %v", err)
				}
				if phase == "recovery conflict" {
					if !errors.Is(err, os.ErrExist) {
						t.Errorf("lost recovery conflict: %v", err)
					}
					assertRestoreTree(t, oldPath, map[string]string{"concurrent.dll": "do not overwrite"})
				} else if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("failed rollback unexpectedly published the original: %v", err)
				}
				assertRestoreTree(t, originalPath, original)
				assertRestoreTree(t, preparedPath, map[string]string{"plugin.dll": "new plugin"})
				if info, err := os.Stat(originalPath); err != nil || info.Mode().Perm() != 0o750 {
					t.Errorf("retained original lost its directory mode: %v, %v", info, err)
				}
			} else {
				if !cleaned {
					t.Error("recoverable failure did not clean its preparation")
				}
				assertRestoreTree(t, oldPath, original)
				if info, err := os.Stat(oldPath); err != nil || info.Mode().Perm() != 0o750 {
					t.Errorf("original directory mode changed: %v, %v", info, err)
				}
				if phase == "restored cleanup" || phase == "preparation cleanup" {
					if !errors.Is(err, cleanupCause) || !errors.Is(err, ErrBackupDeleteFailed) || !strings.Contains(err.Error(), staging) ||
						strings.Contains(err.Error(), "original retained at") {
						t.Errorf("cleanup misreported as failed recovery: %v", err)
					}
				} else if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("failed attempt left unreported staging: %v", err)
				}
			}
			if phase == "retain original" && (!errors.Is(err, syscall.EXDEV) || !errors.Is(err, ErrModDeleteFailed)) {
				t.Errorf("cross-device staging failure lost its cause: %v", err)
			}
			root := oldPath
			if recoveryFailed {
				root = originalPath
			}
			for _, name := range []string{"plugin.dll", "nested/settings"} {
				if info, err := os.Stat(filepath.Join(root, name)); err != nil || info.Mode().Perm() != 0o600 {
					t.Errorf("original file mode changed for %q: %v, %v", name, info, err)
				}
			}
			if _, err := os.Stat(newPath); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("incomplete replacement remains live: %v", err)
			}
			if data, err := os.ReadFile(filepath.Join(m.modDirectory, "unrelated.dll")); err != nil || string(data) != "unrelated DLL" {
				t.Errorf("unrelated plugin changed: %q, %v", data, err)
			}
		})
	}
}

func TestModReplacementMissingOriginal(t *testing.T) {
	for _, phase := range []string{"success", "cross-device publication failure"} {
		t.Run("given missing original files, when replacement reaches "+phase+", then preserve absence or publish without invented recovery", func(t *testing.T) {
			m := NewManager(&pluginHTTPClient{GetFunc: func(string) (*http.Response, error) {
				return &http.Response{Body: io.NopCloser(bytes.NewReader(modRecoveryArchive(t, false)))}, nil
			}}, filepath.Join(restoreTempDir(t), "server")).(*manager)
			destination := filepath.Join(m.modDirectory, "Owner-Plugin-2.0.0")
			handle, err := m.replaceMod("fixture", "Owner-Plugin-1.0.0", "Owner-Plugin-2.0.0", modReplacementOperations{
				rename: func(source, target string) error {
					if target != destination {
						t.Fatalf("missing original triggered an original-file rename: %q -> %q", source, target)
					}
					if phase != "success" {
						return syscall.EXDEV
					}
					return os.Rename(source, target)
				},
				removeArchive: os.Remove, removeAll: os.RemoveAll,
			})
			if phase == "success" {
				if err != nil || handle == nil || handle.RecoveryPath() != "" || handle.Path() != destination {
					t.Fatalf("missing-original replacement = %v, %v", handle, err)
				}
				if err := handle.Commit(); err != nil {
					t.Fatal(err)
				}
				assertRestoreTree(t, m.modDirectory, map[string]string{"Owner-Plugin-2.0.0/plugin.dll": "new plugin"})
			} else {
				if handle != nil || !errors.Is(err, syscall.EXDEV) || errors.Is(err, ErrBackupRestoreFailed) ||
					strings.Contains(err.Error(), "original retained at") {
					t.Errorf("missing-original failure invented recovery: %v, %v", handle, err)
				}
				assertRestoreTree(t, m.modDirectory, map[string]string{})
			}
			if paths, err := filepath.Glob(filepath.Join(filepath.Dir(m.modDirectory), ".warden-replacement-*")); err != nil || len(paths) != 0 {
				t.Errorf("missing-original replacement left staging: %v, %v", paths, err)
			}
		})
	}
}

func TestModReplacementOwnedRecovery(t *testing.T) {
	for _, cleanupFails := range []bool{false, true} {
		name := "given a published replacement, when a nested install runs and recording finishes, then consume only owned recovery"
		if cleanupFails {
			name = "given a recorded replacement, when cleanup partly fails, then keep the installed tree without advertising consumed recovery"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			m := NewManager(&pluginHTTPClient{GetFunc: func(string) (*http.Response, error) {
				return &http.Response{Body: io.NopCloser(bytes.NewReader(modRecoveryArchive(t, false)))}, nil
			}}, filepath.Join(restoreTempDir(t), "server")).(*manager)
			oldPath := filepath.Join(m.modDirectory, "Owner-Plugin-1.0.0")
			writeRestoreTree(t, oldPath, map[string]string{"plugin.dll": "working original", "settings": "original settings"})
			writeRestoreTree(t, m.modDirectory, map[string]string{"unrelated.dll": "unrelated DLL"})
			handle, err := m.ReplaceMod("fixture", "Owner-Plugin-1.0.0", "Owner-Plugin-2.0.0")
			if err != nil {
				t.Fatal(err)
			}
			recovery := handle.RecoveryPath()
			if recovery == "" || strings.HasPrefix(recovery, m.modDirectory+string(filepath.Separator)) {
				t.Fatalf("original not independently retained: %q", recovery)
			}
			if _, err := m.InstallMod("dependency", "Owner-Dependency-1.0.0"); err != nil {
				t.Fatal(err)
			}
			if m.backup.Path() != nil || handle.RecoveryPath() != recovery {
				t.Fatal("nested installation changed replacement snapshot ownership")
			}
			assertRestoreTree(t, recovery, map[string]string{"plugin.dll": "working original", "settings": "original settings"})
			want := map[string]string{
				"unrelated.dll":                     "unrelated DLL",
				"Owner-Plugin-2.0.0/plugin.dll":     "new plugin",
				"Owner-Dependency-1.0.0/plugin.dll": "new plugin",
			}
			assertRestoreTree(t, m.modDirectory, want)
			cause := errors.New("original cleanup interrupted")
			if cleanupFails {
				handle.(*modReplacement).removeAll = func(path string) error {
					if handle.RecoveryPath() != "" || path != filepath.Dir(recovery) {
						t.Fatal("cleanup did not first consume ownership of its original")
					}
					if err := os.Remove(filepath.Join(recovery, "plugin.dll")); err != nil {
						t.Fatal(err)
					}
					return cause
				}
			}
			err = handle.Commit()
			if _, completed := err.(*ModInstallCleanupError); completed != cleanupFails {
				t.Errorf("file completion misclassified: %v", err)
			}
			if cleanupFails {
				if !errors.Is(err, cause) || !errors.Is(err, ErrBackupDeleteFailed) || !strings.Contains(err.Error(), filepath.Dir(recovery)) {
					t.Errorf("cleanup diagnostics lost: %v", err)
				}
				assertRestoreTree(t, recovery, map[string]string{"settings": "original settings"})
			} else if err != nil {
				t.Fatal(err)
			} else if _, err := os.Stat(filepath.Dir(recovery)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("finalized replacement left staging: %v", err)
			}
			if handle.RecoveryPath() != "" || !errors.Is(handle.Commit(), ErrModReplacementFinished) {
				t.Fatal("consumed recovery remained usable")
			}
			assertRestoreTree(t, m.modDirectory, want)
		})
	}
}

func TestModReplacementRejectsUnsafeDestinations(t *testing.T) {
	for _, phase := range []string{"existing destination", "symlinked plugins", "old traversal", "new traversal", "backslash traversal", "empty name", "dot name"} {
		t.Run("given "+phase+", when replacing, then refuse before downloading or changing files", func(t *testing.T) {
			server := filepath.Join(restoreTempDir(t), "server")
			m := NewManager(&pluginHTTPClient{GetFunc: func(string) (*http.Response, error) {
				t.Fatal("unsafe replacement downloaded an archive")
				return nil, nil
			}}, server).(*manager)
			oldName, newName := "Owner-Plugin-1.0.0", "Owner-Plugin-2.0.0"
			writeRestoreTree(t, m.modDirectory, map[string]string{oldName + "/plugin.dll": "working original"})
			want := map[string]string{oldName + "/plugin.dll": "working original"}
			root := m.modDirectory
			switch phase {
			case "existing destination":
				writeRestoreTree(t, root, map[string]string{newName + "/sentinel": "existing destination"})
				want[newName+"/sentinel"] = "existing destination"
			case "symlinked plugins":
				root = filepath.Join(server, "original-plugins")
				if err := os.Rename(m.modDirectory, root); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(root, m.modDirectory); err != nil {
					t.Fatal(err)
				}
			case "old traversal":
				oldName = "../../server"
			case "new traversal":
				newName = "../escape"
			case "backslash traversal":
				newName = `..\escape`
			case "empty name":
				newName = ""
			case "dot name":
				newName = "."
			}
			handle, err := m.ReplaceMod("fixture", oldName, newName)
			if err == nil || handle != nil {
				t.Fatalf("unsafe replacement accepted: %v, %v", handle, err)
			}
			if phase == "existing destination" && !errors.Is(err, os.ErrExist) {
				t.Errorf("destination conflict cause lost: %v", err)
			}
			if phase == "symlinked plugins" && !errors.Is(err, ErrBackupRestoreFailed) {
				t.Errorf("root-symlink preflight policy changed: %v", err)
			}
			assertRestoreTree(t, root, want)
			if matches, err := filepath.Glob(filepath.Join(filepath.Dir(m.modDirectory), ".warden-replacement-*")); err != nil || len(matches) != 0 {
				t.Errorf("refused replacement created staging: %v, %v", matches, err)
			}
		})
	}
}
