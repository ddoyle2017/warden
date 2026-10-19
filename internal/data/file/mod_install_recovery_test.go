package file

import (
	"archive/zip"
	"bytes"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
)

func TestInstallModRecoveryDiagnostics(t *testing.T) {
	for _, phase := range []string{"archive write", "extraction", "partial extraction", "archive deletion"} {
		for _, recovery := range []string{"successful restoration", "failed restoration", "partial cleanup after restoration"} {
			t.Run("given failed "+phase+", when "+recovery+", then report causes and recovery state", func(t *testing.T) {
				server := filepath.Join(restoreTempDir(t), "server")
				backups := t.TempDir()
				t.Setenv("TMPDIR", backups)
				plugins := filepath.Join(server, "BepInEx", "plugins")
				saved := map[string]string{"a-existing.dll": "first original", "nested/z-existing.dll": "second original"}
				writeRestoreTree(t, plugins, saved)
				const fullName = "Owner-Plugin-2.0.0"
				archive := modRecoveryArchive(t, phase == "partial extraction")
				writeCause, deleteCause := errors.New("archive stream interrupted"), errors.New("archive deletion interrupted")
				restoreCause, cleanupCause := errors.New("restore copy interrupted"), errors.New("snapshot cleanup interrupted")
				var body io.Reader = bytes.NewReader(archive)
				wantCause := ErrZipDeleteFailed
				dirty := map[string]string{
					"a-existing.dll": "first original", "nested/z-existing.dll": "second original",
					fullName + ".zip": string(archive), fullName + "/plugin.dll": "new plugin",
				}
				switch phase {
				case "archive write":
					body = io.MultiReader(strings.NewReader("partial archive"), iotest.ErrReader(writeCause))
					wantCause = ErrFileWriteFailed
					dirty[fullName+".zip"] = "partial archive"
					delete(dirty, fullName+"/plugin.dll")
				case "extraction":
					body = strings.NewReader("bad archive")
					wantCause = ErrZipReadFailed
					dirty[fullName+".zip"] = "bad archive"
					delete(dirty, fullName+"/plugin.dll")
				case "partial extraction":
					wantCause = ErrFileWriteFailed
					dirty[fullName+"/corrupt.dll"] = "corrupt plugin"
				}
				m := NewManager(&pluginHTTPClient{GetFunc: func(string) (*http.Response, error) {
					return &http.Response{Body: io.NopCloser(body)}, nil
				}}, server).(*manager)
				b := NewBackup().(*backup)
				var snapshot string
				restoreCalls := 0
				m.backup = &modInstallBackup{
					Backup: b,
					restore: func(destination string) error {
						restoreCalls++
						snapshot = *b.Path()
						assertRestoreTree(t, plugins, dirty)
						return b.restore(destination, restoreOperations{
							copy: func(source, target string) error {
								assertRestoreTree(t, source, saved)
								if recovery == "failed restoration" {
									return restoreCause
								}
								return copyDirectory(source, target)
							},
							rename: os.Rename,
							removeAll: func(path string) error {
								if path == snapshot && recovery == "partial cleanup after restoration" {
									if err := os.Remove(filepath.Join(path, "a-existing.dll")); err != nil {
										t.Fatal(err)
									}
									return cleanupCause
								}
								return os.RemoveAll(path)
							},
						})
					},
					remove: func() error {
						t.Fatal("incomplete installation attempted final cleanup")
						return nil
					},
				}
				path, err := m.installMod("fixture", fullName, func(path string) error {
					if phase != "archive deletion" || path != filepath.Join(plugins, fullName+".zip") {
						t.Fatalf("unexpected archive deletion: %q", path)
					}
					return &os.PathError{Op: "remove", Path: path, Err: deleteCause}
				})
				if !errors.Is(err, wantCause) || path != "" || restoreCalls != 1 {
					t.Fatalf("failed installation = %q, %v, restores=%d", path, err, restoreCalls)
				}
				var completed *ModInstallCleanupError
				if errors.As(err, &completed) {
					t.Errorf("incomplete installation reported file completion: %v", err)
				}
				switch phase {
				case "archive write":
					if !errors.Is(err, writeCause) {
						t.Errorf("archive write cause lost: %v", err)
					}
				case "extraction":
					if !errors.Is(err, zip.ErrFormat) {
						t.Errorf("archive read cause lost: %v", err)
					}
				case "partial extraction":
					if !errors.Is(err, zip.ErrChecksum) {
						t.Errorf("extraction checksum cause lost: %v", err)
					}
				case "archive deletion":
					var cause *os.PathError
					if !errors.Is(err, deleteCause) || !errors.As(err, &cause) || cause.Path != filepath.Join(plugins, fullName+".zip") {
						t.Errorf("archive deletion cause lost: %v", err)
					}
				}
				switch recovery {
				case "failed restoration":
					if !errors.Is(err, ErrBackupRestoreFailed) || !errors.Is(err, restoreCause) ||
						!strings.Contains(err.Error(), "backup retained at") || !strings.Contains(err.Error(), snapshot) {
						t.Errorf("retryable recovery state lost: %v", err)
					}
					if b.Path() == nil || *b.Path() != snapshot {
						t.Fatal("failed restoration consumed its backup")
					}
					assertRestoreTree(t, snapshot, saved)
					assertRestoreTree(t, plugins, dirty)
					if err := b.Restore(plugins); err != nil {
						t.Fatalf("retained snapshot could not restore original files: %v", err)
					}
				case "partial cleanup after restoration":
					if !errors.Is(err, ErrBackupDeleteFailed) || !errors.Is(err, cleanupCause) ||
						!strings.Contains(err.Error(), snapshot) || strings.Contains(err.Error(), "backup retained at") {
						t.Errorf("consumed recovery state reported incorrectly: %v", err)
					}
					assertRestoreTree(t, snapshot, map[string]string{"nested/z-existing.dll": "second original"})
				case "successful restoration":
					if errors.Is(err, ErrBackupRestoreFailed) || errors.Is(err, ErrBackupDeleteFailed) {
						t.Errorf("successful recovery reported failure: %v", err)
					}
				}
				assertRestoreTree(t, plugins, saved)
				if _, err := os.Lstat(filepath.Join(plugins, fullName)); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("restoration left an incomplete release directory: %v", err)
				}
				if b.Path() != nil || !errors.Is(b.Restore(plugins), ErrBackupMissing) {
					t.Error("consumed backup remained retryable")
				}
				if recovery != "partial cleanup after restoration" {
					if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
						t.Errorf("recovery orphaned backups: %v, %v", entries, err)
					}
				}
			})
		}
	}
}

func TestInstallModFinalCleanup(t *testing.T) {
	for name, failCleanup := range map[string]bool{
		"given installed files, when cleanup succeeds, then return the installation path":                            false,
		"given installed files, when backup deletion partially fails, then report completion and consumed artifacts": true,
	} {
		t.Run(name, func(t *testing.T) {
			server := filepath.Join(restoreTempDir(t), "server")
			t.Setenv("TMPDIR", t.TempDir())
			plugins := filepath.Join(server, "BepInEx", "plugins")
			saved := map[string]string{"a-existing.dll": "first original", "z-existing.dll": "second original"}
			writeRestoreTree(t, plugins, saved)
			m := NewManager(&pluginHTTPClient{GetFunc: func(string) (*http.Response, error) {
				return &http.Response{Body: io.NopCloser(bytes.NewReader(modRecoveryArchive(t, false)))}, nil
			}}, server).(*manager)
			b := NewBackup().(*backup)
			cleanupCause := errors.New("snapshot deletion interrupted")
			var snapshot string
			m.backup = &modInstallBackup{
				Backup: b,
				restore: func(string) error {
					t.Fatal("completed installation attempted restoration")
					return nil
				},
				remove: func() error {
					snapshot = *b.Path()
					return b.remove(func(path string) error {
						if b.Path() != nil {
							t.Fatal("cleanup began with an active snapshot")
						}
						if !failCleanup {
							return os.RemoveAll(path)
						}
						if err := os.Remove(filepath.Join(path, "a-existing.dll")); err != nil {
							t.Fatal(err)
						}
						return cleanupCause
					})
				},
			}
			const fullName = "Owner-Plugin-2.0.0"
			path, err := m.InstallMod("fixture", fullName)
			if _, completed := err.(*ModInstallCleanupError); completed != failCleanup {
				t.Errorf("completed-file cleanup classification = %t, error=%v", completed, err)
			}
			if path != filepath.Join(plugins, fullName) {
				t.Errorf("published installation path lost: %q", path)
			}
			saved[fullName+"/plugin.dll"] = "new plugin"
			assertRestoreTree(t, plugins, saved)
			if snapshot == "" || b.Path() != nil || !errors.Is(b.Restore(plugins), ErrBackupMissing) {
				t.Fatal("final cleanup did not consume the backup")
			}
			if failCleanup {
				if !errors.Is(err, ErrBackupDeleteFailed) || !errors.Is(err, cleanupCause) ||
					!strings.Contains(err.Error(), snapshot) || strings.Contains(err.Error(), "backup retained at") {
					t.Errorf("partial cleanup was hidden or reported as recoverable: %v", err)
				}
				assertRestoreTree(t, snapshot, map[string]string{"z-existing.dll": "second original"})
			} else if err != nil {
				t.Errorf("successful cleanup failed: %v", err)
			} else if _, err := os.Stat(snapshot); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("completed cleanup left its snapshot: %v", err)
			}
		})
	}
}

type modInstallBackup struct {
	Backup
	restore func(string) error
	remove  func() error
}

func (b *modInstallBackup) Restore(destination string) error { return b.restore(destination) }
func (b *modInstallBackup) Remove() error                    { return b.remove() }

func modRecoveryArchive(t *testing.T, corrupt bool) []byte {
	t.Helper()
	var body bytes.Buffer
	writer := zip.NewWriter(&body)
	entries := []struct{ name, contents string }{{"plugin.dll", "new plugin"}}
	if corrupt {
		entries = append(entries, struct{ name, contents string }{"corrupt.dll", "corrupt plugin"})
	}
	for _, entry := range entries {
		checksum := crc32.ChecksumIEEE([]byte(entry.contents))
		if entry.name == "corrupt.dll" {
			checksum ^= 1
		}
		out, err := writer.CreateRaw(&zip.FileHeader{
			Name: entry.name, Method: zip.Store, CRC32: checksum,
			CompressedSize64: uint64(len(entry.contents)), UncompressedSize64: uint64(len(entry.contents)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(out, entry.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}
