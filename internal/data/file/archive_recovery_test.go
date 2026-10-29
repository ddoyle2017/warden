package file_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"github.com/ddoyle2017/warden/internal/data/file"
	"github.com/ddoyle2017/warden/internal/test/helper"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateBepInExArchiveRejectionRecovery(t *testing.T) {
	tests := map[string]bool{
		"given a valid archive, when updating, then replace framework bytes while preserving user data":                           false,
		"given a checksum failure after an earlier entry, when updating, then restore the server and retain plugin recovery data": true,
	}
	for name, corrupt := range tests {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			server, backups := filepath.Join(parent, "server"), filepath.Join(parent, "backups")
			th := helper.NewHelper(t, helper.WithValheimFolder(server))
			if err := os.Mkdir(backups, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TMPDIR", backups)
			original := map[string]string{
				"BepInEx/core/runtime.dll":                "original framework",
				"BepInEx/config/BepInEx.cfg":              "user framework configuration",
				"BepInEx/config/nested/mod.json":          "user mod configuration",
				"BepInEx/plugins/existing/plugin.dll":     "existing plugin",
				"BepInEx/plugins/existing/data/state.bin": "existing plugin state",
				"doorstop_config.ini":                     "original loader",
				"start_server_bepinex.sh":                 "original startup",
				"worlds/world.db":                         "unrelated world data",
			}
			writeArchiveRecoveryFiles(t, server, original)
			body := &archiveRecoveryBody{reader: bytes.NewReader(recoveryArchive(t, true, corrupt))}
			requests := 0
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				requests++
				if url != helper.TestDownloadURL {
					t.Fatalf("unexpected URL %q", url)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			}}

			err := file.NewManager(client, server).UpdateBepInEx(helper.TestDownloadURL, "Test-Framework-2.0.0")
			if corrupt {
				for _, cause := range []error{file.ErrFrameworkUpdateFailed, file.ErrFileWriteFailed, zip.ErrChecksum} {
					if !errors.Is(err, cause) {
						t.Errorf("update error = %v, want cause %v", err, cause)
					}
				}
				var completed *file.FrameworkMutationCleanupError
				if errors.As(err, &completed) {
					t.Errorf("rejected archive incorrectly marks framework replacement complete: %v", err)
				}
				assertArchiveRecoveryFiles(t, th, server, original, nil)
			} else {
				if err != nil {
					t.Fatalf("valid update failed: %v", err)
				}
				assertArchiveRecoveryFiles(t, th, server, map[string]string{
					"BepInEx/core/runtime.dll":                "replacement framework",
					"BepInEx/config/BepInEx.cfg":              "user framework configuration",
					"BepInEx/config/nested/mod.json":          "user mod configuration",
					"BepInEx/config/new-default.cfg":          "new packaged default",
					"BepInEx/plugins/existing/plugin.dll":     "existing plugin",
					"BepInEx/plugins/existing/data/state.bin": "existing plugin state",
					"BepInEx/plugins/bundled/plugin.dll":      "bundled plugin",
					"doorstop_config.ini":                     "replacement loader",
					"start_server_bepinex.sh":                 "replacement startup",
					"worlds/world.db":                         "unrelated world data",
				}, nil)
			}
			if requests != 1 || body.bytesRead == 0 || !body.closed {
				t.Errorf("archive lifecycle: requests=%d, bytes read=%d, closed=%v", requests, body.bytesRead, body.closed)
			}
			config := filepath.Join(server, "BepInEx", "config", "BepInEx.cfg")
			if info, statErr := os.Stat(config); statErr != nil || info.Mode().Perm() != 0o600 {
				t.Errorf("private configuration mode changed: %v, %v", info, statErr)
			}
			if entries, readErr := os.ReadDir(backups); readErr != nil || len(entries) != 0 {
				t.Errorf("completed update or restoration left server snapshots: %v, %v", entries, readErr)
			}
			stages, globErr := filepath.Glob(filepath.Join(parent, ".warden-plugins-*"))
			if globErr != nil {
				t.Fatal(globErr)
			}
			if !corrupt {
				if len(stages) != 0 {
					t.Errorf("successful update retained plugin recovery directories: %v", stages)
				}
				return
			}
			if len(stages) != 1 {
				t.Fatalf("rejected archive should retain one independent plugin snapshot, got %v", stages)
			}
			if err == nil || !strings.Contains(err.Error(), stages[0]) {
				t.Errorf("update error does not report the retained plugin path: %v", err)
			}
			assertArchiveRecoveryFiles(t, th, stages[0], map[string]string{
				"existing/plugin.dll":     "existing plugin",
				"existing/data/state.bin": "existing plugin state",
			}, nil)
		})
	}
}

func TestInstallArchiveBackupPreflight(t *testing.T) {
	tests := map[string]struct {
		framework bool
		failCopy  bool
	}{
		"given a valid snapshot, when installing a mod, then install the payload and retain unrelated files": {},
		"given a partial snapshot failure, when installing a mod, then stop before reading the archive or changing live files": {
			failCopy: true,
		},
		"given a valid snapshot, when installing a framework, then install the payload and retain unrelated files": {
			framework: true,
		},
		"given a partial snapshot failure, when installing a framework, then stop before reading the archive or changing live files": {
			framework: true, failCopy: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			server, backups := filepath.Join(parent, "server"), filepath.Join(parent, "backups")
			th := helper.NewHelper(t, helper.WithValheimFolder(server))
			if err := os.Mkdir(backups, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TMPDIR", backups)
			prefix := filepath.Join("BepInEx", "plugins")
			if tt.framework {
				prefix = ""
			}
			original := map[string]string{
				filepath.Join(prefix, "a-copied-first"): "first copied bytes",
				filepath.Join(prefix, "z-existing"):     "later live bytes",
				"worlds/world.db":                       "unrelated world data",
			}
			writeArchiveRecoveryFiles(t, server, original)
			links := map[string]string{}
			if tt.failCopy {
				link := filepath.Join(prefix, "m-dangling")
				target := filepath.Join(parent, "missing-target")
				if err := os.Symlink(target, filepath.Join(server, link)); err != nil {
					t.Fatal(err)
				}
				links[link] = target
			}
			body := &archiveRecoveryBody{reader: bytes.NewReader(recoveryArchive(t, tt.framework, false))}
			requests := 0
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				requests++
				if url != helper.TestDownloadURL {
					t.Fatalf("unexpected URL %q", url)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			}}
			manager := file.NewManager(client, server)
			const fullName = "Test-Package-2.0.0"
			var path string
			var err error
			if tt.framework {
				path, err = manager.InstallBepInEx(helper.TestDownloadURL, fullName)
			} else {
				path, err = manager.InstallMod(helper.TestDownloadURL, fullName)
			}
			if requests > 0 && !body.closed {
				t.Errorf("download lifecycle: requests=%d, closed=%v", requests, body.closed)
			}
			if tt.failCopy {
				if !errors.Is(err, file.ErrBackupCreateFailed) || !errors.Is(err, file.ErrFileOpenFailed) {
					t.Errorf("install error = %v, want snapshot failure with its copy cause", err)
				}
				var completed *file.FrameworkMutationCleanupError
				if errors.As(err, &completed) {
					t.Errorf("failed snapshot incorrectly marks installation complete: %v", err)
				}
				if path != "" || body.bytesRead != 0 {
					t.Errorf("failed snapshot proceeded with archive: path=%q, bytes read=%d", path, body.bytesRead)
				}
				assertArchiveRecoveryFiles(t, th, server, original, links)
			} else {
				wantPath := filepath.Join(server, prefix, fullName)
				if tt.framework {
					wantPath = server
				}
				if err != nil || path != wantPath || requests != 1 || body.bytesRead == 0 {
					t.Fatalf("valid install: path=%q, error=%v, requests=%d, bytes read=%d", path, err, requests, body.bytesRead)
				}
				expected := make(map[string]string, len(original)+6)
				for name, contents := range original {
					expected[name] = contents
				}
				if tt.framework {
					for name, contents := range map[string]string{
						"BepInEx/config/BepInEx.cfg":         "packaged framework configuration",
						"BepInEx/core/runtime.dll":           "replacement framework",
						"BepInEx/config/new-default.cfg":     "new packaged default",
						"BepInEx/plugins/bundled/plugin.dll": "bundled plugin",
						"doorstop_config.ini":                "replacement loader",
						"start_server_bepinex.sh":            "replacement startup",
					} {
						expected[name] = contents
					}
				} else {
					expected[filepath.Join(prefix, fullName, "plugin.dll")] = "replacement plugin"
				}
				assertArchiveRecoveryFiles(t, th, server, expected, nil)
			}
			if entries, readErr := os.ReadDir(backups); readErr != nil || len(entries) != 0 {
				t.Errorf("install left an incomplete or consumed backup: %v, %v", entries, readErr)
			}
		})
	}
}

func recoveryArchive(t *testing.T, framework, corrupt bool) []byte {
	t.Helper()
	entries := []archiveEntry{{name: "plugin.dll", contents: "replacement plugin"}}
	if framework {
		entries = []archiveEntry{
			{name: "BepInExPack_Valheim/BepInEx/config/BepInEx.cfg", contents: "packaged framework configuration"},
			{name: "BepInExPack_Valheim/BepInEx/core/runtime.dll", contents: "replacement framework"},
			{name: "BepInExPack_Valheim/BepInEx/config/new-default.cfg", contents: "new packaged default"},
			{name: "BepInExPack_Valheim/BepInEx/plugins/bundled/plugin.dll", contents: "bundled plugin"},
			{name: "BepInExPack_Valheim/doorstop_config.ini", contents: "replacement loader"},
			{name: "BepInExPack_Valheim/start_server_bepinex.sh", contents: "replacement startup"},
		}
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		checksum := crc32.ChecksumIEEE([]byte(entry.contents))
		if corrupt && strings.HasSuffix(entry.name, "/core/runtime.dll") {
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
	return buffer.Bytes()
}

func writeArchiveRecoveryFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func assertArchiveRecoveryFiles(t *testing.T, th helper.Helper, root string, want, links map[string]string) {
	t.Helper()
	actual, err := th.GetFileList(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != len(want)+len(links) {
		t.Errorf("file set at %q = %v, want files %v and links %v", root, actual, want, links)
	}
	for name, contents := range want {
		if data, err := os.ReadFile(filepath.Join(root, name)); err != nil || string(data) != contents {
			t.Errorf("file %q = %q, %v; want %q", name, data, err, contents)
		}
	}
	for name, target := range links {
		if got, err := os.Readlink(filepath.Join(root, name)); err != nil || got != target {
			t.Errorf("symlink %q = %q, %v; want %q", name, got, err, target)
		}
	}
}

type archiveRecoveryBody struct {
	reader    io.Reader
	bytesRead int
	closed    bool
}

func (body *archiveRecoveryBody) Read(p []byte) (int, error) {
	n, err := body.reader.Read(p)
	body.bytesRead += n
	return n, err
}

func (body *archiveRecoveryBody) Close() error {
	body.closed = true
	return nil
}
