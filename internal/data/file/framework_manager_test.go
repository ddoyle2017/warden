package file_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/api"
	"warden/internal/data/file"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestUpdateBepInExPluginStaging(t *testing.T) {
	if os.Getenv("WARDEN_TEST_PLUGIN_STAGING_CHILD") != "1" {
		// A child process fixes the umask without changing global test state.
		cmd := exec.Command("sh", "-c", `umask 0022; exec "$@"`, "sh", os.Args[0],
			"-test.run=^TestUpdateBepInExPluginStaging$", "-test.count=1")
		cmd.Env = append(os.Environ(), "WARDEN_TEST_PLUGIN_STAGING_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("plugin staging with umask 0022: %v\n%s", err, output)
		}
		return
	}

	control := t.TempDir()
	original := filepath.Join(control, "original")
	if err := os.Mkdir(original, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(original, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, filepath.Join(control, "moved")); err != nil {
		t.Fatal(err)
	}
	assertPluginDirectoryModes(t, control, map[string]os.FileMode{"moved": 0o775})

	tests := map[string]struct {
		missingPlugins bool
		emptyPlugins   bool
		invalidArchive bool
		expected       error
	}{
		"given installed plugins, when updating, then retain their bytes and modes alongside bundled plugins": {},
		"given an empty plugins directory, when updating, then install bundled plugins": {
			emptyPlugins: true,
		},
		"given no plugins directory, when staging fails, then leave the server and complete backup intact": {
			missingPlugins: true, expected: os.ErrNotExist,
		},
		"given an invalid archive, when framework restoration runs, then retain the independent plugin snapshot": {
			invalidArchive: true, expected: file.ErrZipReadFailed,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			parent, backups := t.TempDir(), t.TempDir()
			server := filepath.Join(parent, "server")
			th := helper.NewHelper(t, helper.WithValheimFolder(server))
			t.Setenv("TMPDIR", backups)
			files := map[string]string{"server-data": "unrelated bytes", "BepInEx/core/old": "old framework"}
			plugins := map[string]string{"a-plugin": "plugin bytes", "nested/deeper/config": "plugin configuration"}
			directoryModes := map[string]os.FileMode{"nested": 0o775, "nested/deeper": 0o775, "empty": 0o775}
			if tt.emptyPlugins {
				plugins = map[string]string{}
				directoryModes = map[string]os.FileMode{}
			}
			if !tt.missingPlugins {
				for name, contents := range plugins {
					files[filepath.Join("BepInEx", "plugins", name)] = contents
				}
			}
			for name, contents := range files {
				path := filepath.Join(server, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if !tt.missingPlugins {
				root := filepath.Join(server, "BepInEx", "plugins")
				if err := os.MkdirAll(root, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(root, 0o775); err != nil {
					t.Fatal(err)
				}
				for name, mode := range directoryModes {
					path := filepath.Join(root, name)
					if err := os.MkdirAll(path, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(path, mode); err != nil {
						t.Fatal(err)
					}
				}
				if !tt.emptyPlugins {
					if err := os.Chmod(filepath.Join(root, "a-plugin"), 0o751); err != nil {
						t.Fatal(err)
					}
				}
			}
			requests := 0
			var bundledPlugin []byte
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				requests++
				if url != helper.TestDownloadURL {
					t.Fatalf("unexpected download URL %q", url)
				}
				stages, err := filepath.Glob(filepath.Join(parent, ".warden-plugins-*"))
				if err != nil || len(stages) != 1 {
					t.Fatalf("plugin snapshot did not survive framework removal: %v, %v", stages, err)
				}
				assertPluginFiles(t, th, stages[0], plugins)
				assertPluginDirectoryModes(t, stages[0], map[string]os.FileMode{".": 0o700})
				assertPluginDirectoryModes(t, stages[0], directoryModes)
				if tt.invalidArchive {
					return &http.Response{Body: io.NopCloser(strings.NewReader("invalid archive"))}, nil
				}
				archive, err := os.ReadFile(filepath.Join(th.GetDataDirectory(), helper.TestBepInExFullName+file.ZipFileExtension))
				if err != nil {
					t.Fatal(err)
				}
				contents, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
				if err != nil {
					t.Fatal(err)
				}
				bundledPlugin, err = fs.ReadFile(contents, "BepInExPack_Valheim/BepInEx/plugins/Valheim.DisplayBepInExInfo.dll")
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{Body: io.NopCloser(bytes.NewReader(archive))}, nil
			}}
			err := file.NewManager(client, server).UpdateBepInEx(helper.TestDownloadURL, helper.TestBepInExFullName)
			if tt.expected != nil {
				if !errors.Is(err, file.ErrFrameworkUpdateFailed) || !errors.Is(err, tt.expected) {
					t.Fatalf("expected update failure wrapping %v, got %v", tt.expected, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			stages, globErr := filepath.Glob(filepath.Join(parent, ".warden-plugins-*"))
			if globErr != nil {
				t.Fatal(globErr)
			}
			if tt.missingPlugins {
				if requests != 0 || len(stages) != 0 {
					t.Errorf("failed staging continued or left a partial copy: requests=%d, stages=%v", requests, stages)
				}
				assertPluginFiles(t, th, server, files)
				snapshots, globErr := filepath.Glob(filepath.Join(backups, "warden-backup*"))
				if globErr != nil || len(snapshots) != 1 {
					t.Fatalf("expected a retained server backup: %v, %v", snapshots, globErr)
				}
				th.VerifyBackup(server, snapshots[0])
				if !strings.Contains(err.Error(), snapshots[0]) {
					t.Errorf("staging error omits the server backup path: %v", err)
				}
				return
			}
			if requests != 1 {
				t.Errorf("expected one fixture download, got %d", requests)
			}
			pluginRoot := filepath.Join(server, "BepInEx", "plugins")
			if tt.invalidArchive {
				if len(stages) != 1 {
					t.Fatalf("failed update lost its plugin snapshot: %v", stages)
				}
				if !strings.Contains(err.Error(), stages[0]) {
					t.Errorf("update hid the plugin recovery path: %v", err)
				}
				if _, err := os.Stat(filepath.Join(server, helper.TestBepInExFullName+file.ZipFileExtension)); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("framework restoration left the failed archive: %v", err)
				}
				pluginRoot = stages[0]
			} else {
				if len(stages) != 0 {
					t.Errorf("successful update retained plugin staging: %v", stages)
				}
				plugins["Valheim.DisplayBepInExInfo.dll"] = string(bundledPlugin)
				if data, err := os.ReadFile(filepath.Join(server, "BepInEx", "core", "BepInEx.dll")); err != nil || len(data) == 0 {
					t.Errorf("new framework was not installed: %v", err)
				}
			}
			assertPluginFiles(t, th, pluginRoot, plugins)
			assertPluginDirectoryModes(t, pluginRoot, directoryModes)
			if !tt.emptyPlugins {
				if info, err := os.Stat(filepath.Join(pluginRoot, "a-plugin")); err != nil || info.Mode().Perm() != 0o751 {
					t.Errorf("plugin mode was not preserved: %v, %v", info, err)
				}
			}
			if data, err := os.ReadFile(filepath.Join(server, "server-data")); err != nil || string(data) != "unrelated bytes" {
				t.Errorf("update changed unrelated server bytes: %q, %v", data, err)
			}
		})
	}
}

func TestUpdateBepInExPartialPluginTransfer(t *testing.T) {
	parent := t.TempDir()
	server := filepath.Join(parent, "server")
	th := helper.NewHelper(t, helper.WithValheimFolder(server))
	t.Setenv("TMPDIR", t.TempDir())
	plugins := map[string]string{"a-plugin": "first plugin", "z-plugin/original": "last plugin"}
	for name, contents := range plugins {
		path := filepath.Join(server, "BepInEx", "plugins", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, name := range []string{
		"BepInExPack_Valheim/", "BepInExPack_Valheim/BepInEx/",
		"BepInExPack_Valheim/BepInEx/plugins/", "BepInExPack_Valheim/BepInEx/plugins/z-plugin/",
		"BepInExPack_Valheim/BepInEx/plugins/z-plugin/bundled",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(name, "/") {
			if _, err := io.WriteString(entry, "new bundled plugin"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	client := &mock.HTTPClient{GetFunc: func(string) (*http.Response, error) {
		return &http.Response{Body: io.NopCloser(bytes.NewReader(archive.Bytes()))}, nil
	}}
	err := file.NewManager(client, server).UpdateBepInEx(helper.TestDownloadURL, helper.TestBepInExFullName)
	if !errors.Is(err, file.ErrFrameworkUpdateFailed) || !errors.Is(err, file.ErrFileRenameFailed) {
		t.Fatalf("expected partial plugin transfer failure, got %v", err)
	}
	stages, globErr := filepath.Glob(filepath.Join(parent, ".warden-plugins-*"))
	if globErr != nil || len(stages) != 1 {
		t.Fatalf("expected a retained complete plugin snapshot: %v, %v", stages, globErr)
	}
	if !strings.Contains(err.Error(), stages[0]) {
		t.Errorf("update error omits the plugin recovery path: %v", err)
	}
	assertPluginFiles(t, th, stages[0], plugins)
	assertPluginFiles(t, th, filepath.Join(server, "BepInEx", "plugins"), map[string]string{
		"a-plugin": "first plugin", "z-plugin/bundled": "new bundled plugin",
	})
}

func assertPluginFiles(t *testing.T, th helper.Helper, root string, want map[string]string) {
	t.Helper()
	files, err := th.GetFileList(root)
	if err != nil {
		t.Fatal(err)
	}
	if !th.AreFileListsEqual(want, files) {
		t.Errorf("file set at %q = %v, want %v", root, files, want)
	}
	for name, contents := range want {
		if data, err := os.ReadFile(filepath.Join(root, name)); err != nil || string(data) != contents {
			t.Errorf("file %q = %q, %v; want %q", name, data, err, contents)
		}
	}
}

func assertPluginDirectoryModes(t *testing.T, root string, want map[string]os.FileMode) {
	t.Helper()
	for name, mode := range want {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Errorf("inspect directory %q at %q: %v", name, root, err)
			continue
		}
		if !info.IsDir() || info.Mode().Perm() != mode {
			t.Errorf("directory %q at %q has mode %v; want directory mode %04o", name, root, info.Mode(), mode)
		}
	}
}

func TestInstallBepInEx_Happy(t *testing.T) {
	th := helper.NewHelper(t)
	t.Setenv("TMPDIR", t.TempDir())
	sentinel := filepath.Join(th.GetValheimDirectory(), "server-data")
	if err := os.WriteFile(sentinel, []byte("unrelated server bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	archive, err := os.Open(filepath.Join(th.GetDataDirectory(), helper.TestBepInExFullName+file.ZipFileExtension))
	if err != nil {
		t.Fatalf("unexpected error reading BepInEx zip file, received err: %+v", err)
	}
	client := mock.HTTPClient{
		GetFunc: func(_ string) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       archive,
			}, nil
		},
	}
	m := file.NewManager(&client, th.GetValheimDirectory())

	path, err := m.InstallBepInEx(helper.TestDownloadURL, helper.TestBepInExFullName)
	if err != nil {
		t.Fatalf("expected a nil error, received: %+v", err)
	}
	if path != th.GetValheimDirectory() {
		t.Fatalf("expected path: %s, received: %s", th.GetValheimDirectory(), path)
	}
	if data, err := os.ReadFile(filepath.Join(path, "BepInEx", "core", "BepInEx.dll")); err != nil || len(data) == 0 {
		t.Errorf("framework files were not installed: %v", err)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "unrelated server bytes" {
		t.Errorf("installation changed unrelated files: %q, %v", data, err)
	}

	t.Cleanup(func() {
		th.RemoveServerFiles()
	})
}

func TestInstallBepInEx_Sad(t *testing.T) {
	th := helper.NewHelper(t)

	tests := map[string]struct {
		fullName string
		client   api.HTTPClient
		expected error
	}{
		"returns error when unable to download BepInEx": {
			fullName: helper.TestModFullName,
			client: &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{}, http.ErrContentLength
				},
			},
			expected: api.ErrHTTPClient,
		},
		"returns error when unable to create BepInEx zip archive": {
			fullName: "dba\\.. dwanu^&%* d//]\\",
			client: &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("garbagedata")),
					}, nil
				},
			},
			expected: file.ErrFileCreateFailed,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			manager := file.NewManager(tt.client, th.GetValheimDirectory())

			path, err := manager.InstallBepInEx(helper.TestDownloadURL, tt.fullName)
			if !errors.Is(err, tt.expected) {
				t.Errorf("expected error: %+v, received error: %+v", tt.expected, err)
			}
			if path != "" {
				t.Errorf("expected an empty path, received: %s", path)
			}

			t.Cleanup(func() {
				th.RemoveServerFiles()
			})
		})
	}
}

func TestRemoveBepInEx_Happy(t *testing.T) {
	th := helper.NewHelper(t)

	tests := map[string]struct {
		setUp func(t *testing.T)
	}{
		"if there are no BepInEx files to remove, return successful": {
			setUp: func(t *testing.T) {},
		},
		"if there are BepInEx files, remove everything and return successful": {
			setUp: func(t *testing.T) {
				source := filepath.Join(th.GetDataDirectory(), helper.TestBepInExFullName+file.ZipFileExtension)
				destination := filepath.Join(th.GetValheimDirectory())

				if err := file.Unzip(source, destination); err != nil {
					t.Errorf("unexpected error creating test BepInEx install, received error: %+v", err)
				}

				path := filepath.Join(th.GetValheimDirectory(), file.BepInExContentsDirectory)

				entries, err := os.ReadDir(path)
				if err != nil {
					t.Errorf("unexpected error reading BepInEx test files, received error: %+v", err)
				}
				for _, e := range entries {
					source := filepath.Join(path, e.Name())
					dest := filepath.Join(th.GetValheimDirectory(), e.Name())

					if err := os.Rename(source, dest); err != nil {
						t.Errorf("unexpected error moving BepInEx files, received error: %+v", err)
					}
				}
				if err := os.RemoveAll(path); err != nil {
					t.Errorf("unexpected error cleaning up BepInEx files, received error: %+v", err)
				}
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			test.setUp(t)

			m := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())

			if err := m.RemoveBepInEx(); err != nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}
		})
	}
}
