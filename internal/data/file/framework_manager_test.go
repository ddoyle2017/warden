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
	"path"
	"path/filepath"
	"sort"
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
		missingPlugins    bool
		emptyPlugins      bool
		downloadFailure   bool
		invalidArchive    bool
		incompleteArchive bool
		expected          error
	}{
		"given installed plugins, when updating, then retain their bytes and modes alongside bundled plugins": {},
		"given an empty plugins directory, when updating, then install bundled plugins": {
			emptyPlugins: true,
		},
		"given no plugins directory, when staging fails, then leave the server and complete backup intact": {
			missingPlugins: true, expected: os.ErrNotExist,
		},
		"given a download failure after removal, when updating, then restore the original server": {
			downloadFailure: true, expected: api.ErrHTTPClient,
		},
		"given an invalid archive, when updating, then restore the original server and retain the plugin snapshot": {
			invalidArchive: true, expected: file.ErrZipReadFailed,
		},
		"given an archive without the framework wrapper, when installation fails, then restore the original server": {
			incompleteArchive: true, expected: file.ErrFrameworkInstallFailed,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			parent, backups := t.TempDir(), t.TempDir()
			server := filepath.Join(parent, "server")
			th := helper.NewHelper(t, helper.WithValheimFolder(server))
			t.Setenv("TMPDIR", backups)
			files := map[string]string{
				"server-data":               "unrelated bytes",
				"BepInEx/core/old":          "old framework",
				"BepInEx/config/custom.cfg": "user configuration",
				"doorstop_config.ini":       "old loader configuration",
			}
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
				if _, err := os.Stat(filepath.Join(server, "BepInEx")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("download did not follow framework removal: %v", err)
				}
				snapshots, err := filepath.Glob(filepath.Join(backups, "warden-backup*"))
				if err != nil || len(snapshots) != 1 {
					t.Fatalf("expected the original server snapshot during replacement: %v, %v", snapshots, err)
				}
				assertPluginFiles(t, th, snapshots[0], files)
				if tt.downloadFailure {
					return nil, http.ErrContentLength
				}
				if tt.invalidArchive {
					return &http.Response{Body: io.NopCloser(strings.NewReader("invalid archive"))}, nil
				}
				if tt.incompleteArchive {
					var archive bytes.Buffer
					writer := zip.NewWriter(&archive)
					entry, err := writer.Create("replacement-only")
					if err != nil {
						t.Fatal(err)
					}
					if _, err := io.WriteString(entry, "incomplete replacement"); err != nil {
						t.Fatal(err)
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
					return &http.Response{Body: io.NopCloser(bytes.NewReader(archive.Bytes()))}, nil
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
				var completed *file.FrameworkMutationCleanupError
				if errors.As(err, &completed) {
					t.Errorf("incomplete update was reported as completed: %v", err)
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
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("completed update or rollback orphaned server backups: %v, %v", entries, err)
			}
			pluginRoot := filepath.Join(server, "BepInEx", "plugins")
			if tt.expected != nil {
				if len(stages) != 1 {
					t.Fatalf("failed update lost its plugin snapshot: %v", stages)
				}
				if !strings.Contains(err.Error(), stages[0]) {
					t.Errorf("update hid the plugin recovery path: %v", err)
				}
				if errors.Is(err, file.ErrBackupMissing) {
					t.Errorf("nested framework work consumed the update backup: %v", err)
				}
				assertPluginFiles(t, th, server, files)
				pluginRoot = stages[0]
			} else {
				if len(stages) != 0 {
					t.Errorf("successful update retained plugin staging: %v", stages)
				}
				plugins["Valheim.DisplayBepInExInfo.dll"] = string(bundledPlugin)
				if data, err := os.ReadFile(filepath.Join(server, "BepInEx", "core", "BepInEx.dll")); err != nil || len(data) == 0 {
					t.Errorf("new framework was not installed: %v", err)
				}
				config := filepath.Join(server, "BepInEx", "config", "custom.cfg")
				if data, err := os.ReadFile(config); err != nil || string(data) != "user configuration" {
					t.Errorf("update lost existing configuration: %q, %v", data, err)
				}
				if info, err := os.Stat(config); err != nil || info.Mode().Perm() != 0o600 {
					t.Errorf("configuration mode was not preserved: %v, %v", info, err)
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
	backups := t.TempDir()
	t.Setenv("TMPDIR", backups)
	plugins := map[string]string{"a-plugin": "first plugin", "z-plugin/original": "last plugin"}
	files := map[string]string{
		"BepInEx/core/runtime":      "old framework",
		"BepInEx/config/custom.cfg": "user configuration",
		"doorstop_config.ini":       "old loader configuration",
		"server-data":               "unrelated bytes",
	}
	for name, contents := range plugins {
		files[filepath.Join("BepInEx", "plugins", name)] = contents
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
	var completed *file.FrameworkMutationCleanupError
	if errors.As(err, &completed) {
		t.Errorf("partial plugin transfer was reported as a completed update: %v", err)
	}
	stages, globErr := filepath.Glob(filepath.Join(parent, ".warden-plugins-*"))
	if globErr != nil || len(stages) != 1 {
		t.Fatalf("expected a retained complete plugin snapshot: %v, %v", stages, globErr)
	}
	if !strings.Contains(err.Error(), stages[0]) {
		t.Errorf("update error omits the plugin recovery path: %v", err)
	}
	assertPluginFiles(t, th, stages[0], plugins)
	assertPluginFiles(t, th, server, files)
	if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
		t.Errorf("rollback orphaned server backups: %v, %v", entries, err)
	}
}

func TestUpdateBepInExConfiguration(t *testing.T) {
	defaults := map[string]string{
		"BepInEx.cfg":         "packaged framework defaults",
		"new.cfg":             "new packaged configuration",
		"nested/default.json": "{\"new\": true}\n",
	}
	tests := map[string]struct {
		existing map[string]string
		packaged map[string]string
		want     map[string]string
		conflict bool
	}{
		"given existing configuration, when updating, then retain user bytes and modes alongside new defaults": {
			existing: map[string]string{
				"BepInEx.cfg":  "[Logging.Console]\r\nEnabled = false\r\n",
				"Review.cfg":   "private mod configuration",
				"nested/state": "\x00\xffarbitrary bytes\n",
				"empty":        "",
			},
			packaged: defaults,
			want: map[string]string{
				"BepInEx.cfg":         "[Logging.Console]\r\nEnabled = false\r\n",
				"Review.cfg":          "private mod configuration",
				"nested/state":        "\x00\xffarbitrary bytes\n",
				"empty":               "",
				"new.cfg":             "new packaged configuration",
				"nested/default.json": "{\"new\": true}\n",
			},
		},
		"given missing configuration, when updating, then keep packaged defaults": {
			packaged: defaults, want: defaults,
		},
		"given empty configuration, when updating, then keep packaged defaults": {
			existing: map[string]string{}, packaged: defaults, want: defaults,
		},
		"given no packaged configuration, when updating, then restore the existing tree": {
			existing: map[string]string{"nested/user": "saved settings"},
			want:     map[string]string{"nested/user": "saved settings"},
		},
		"given a user file conflicting with a packaged directory, when preservation partially fails, then restore the original server": {
			existing: map[string]string{"a.cfg": "old settings", "z-conflict": "user file"},
			packaged: map[string]string{"a.cfg": "new defaults", "z-conflict/default.cfg": "packaged settings"},
			conflict: true,
		},
		"given a user directory conflicting with a packaged file, when preservation fails, then restore the original server": {
			existing: map[string]string{"z-conflict/user.cfg": "user settings"},
			packaged: map[string]string{"z-conflict": "packaged file"},
			conflict: true,
		},
		"given a packaged file at the configuration root, when preservation fails, then restore the original server": {
			existing: map[string]string{"user.cfg": "user settings"},
			packaged: map[string]string{"": "packaged file"},
			conflict: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			parent, backups := t.TempDir(), t.TempDir()
			server := filepath.Join(parent, "server")
			th := helper.NewHelper(t, helper.WithValheimFolder(server))
			t.Setenv("TMPDIR", backups)
			original := map[string]string{
				"BepInEx/core/old":            "original framework",
				"BepInEx/plugins/user/config": "plugin-local settings",
				"doorstop_config.ini":         "old loader configuration",
				"server-data":                 "unrelated server bytes",
			}
			for name, contents := range tt.existing {
				original[filepath.Join("BepInEx", "config", name)] = contents
			}
			for name, contents := range original {
				target := filepath.Join(server, name)
				if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			configRoot := filepath.Join(server, "BepInEx", "config")
			if tt.existing != nil {
				if err := os.MkdirAll(configRoot, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			packaged := map[string]string{
				"BepInEx/core/runtime":    "replacement framework",
				"BepInEx/plugins/bundled": "bundled plugin",
				"doorstop_config.ini":     "new loader configuration",
			}
			for name, contents := range tt.packaged {
				packaged[filepath.Join("BepInEx", "config", name)] = contents
			}
			archive := frameworkConfigArchive(t, packaged)
			requests := 0
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				requests++
				if url != helper.TestDownloadURL {
					t.Fatalf("unexpected download URL %q", url)
				}
				return &http.Response{Body: io.NopCloser(bytes.NewReader(archive))}, nil
			}}
			err := file.NewManager(client, server).UpdateBepInEx(helper.TestDownloadURL, helper.TestBepInExFullName)
			stages, globErr := filepath.Glob(filepath.Join(parent, ".warden-plugins-*"))
			if globErr != nil {
				t.Fatal(globErr)
			}
			if tt.conflict {
				var pathErr *os.PathError
				var linkErr *os.LinkError
				if !errors.Is(err, file.ErrFrameworkUpdateFailed) {
					t.Fatalf("expected a configuration conflict causing update failure, got %v", err)
				}
				var conflictPath string
				if errors.As(err, &pathErr) {
					conflictPath = pathErr.Path
				} else if errors.As(err, &linkErr) {
					conflictPath = linkErr.New
				} else {
					t.Fatalf("expected a filesystem conflict, got %v", err)
				}
				if !strings.Contains(conflictPath, string(filepath.Separator)+"config") {
					t.Errorf("failure did not originate in configuration preservation: %v", err)
				}
				assertPluginFiles(t, th, server, original)
				if len(stages) != 1 || !strings.Contains(err.Error(), stages[0]) {
					t.Fatalf("failure lost or hid the independent plugin snapshot: %v, %v", stages, err)
				}
				assertPluginFiles(t, th, stages[0], map[string]string{"user/config": "plugin-local settings"})
			} else {
				if err != nil {
					t.Fatal(err)
				}
				want := map[string]string{
					"BepInEx/core/runtime":        "replacement framework",
					"BepInEx/plugins/bundled":     "bundled plugin",
					"BepInEx/plugins/user/config": "plugin-local settings",
					"doorstop_config.ini":         "new loader configuration",
					"server-data":                 "unrelated server bytes",
				}
				for name, contents := range tt.want {
					want[filepath.Join("BepInEx", "config", name)] = contents
				}
				assertPluginFiles(t, th, server, want)
				if len(stages) != 0 {
					t.Errorf("successful update retained plugin staging: %v", stages)
				}
			}
			for name := range tt.existing {
				target := filepath.Join(configRoot, name)
				if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o600 {
					t.Errorf("private configuration mode changed at %q: %v, %v", target, info, err)
				}
				if info, err := os.Stat(filepath.Dir(target)); err != nil || info.Mode().Perm() != 0o700 {
					t.Errorf("private configuration directory mode changed: %v, %v", info, err)
				}
			}
			if tt.existing != nil {
				if info, err := os.Stat(configRoot); err != nil || info.Mode().Perm() != 0o700 {
					t.Errorf("configuration root mode changed: %v, %v", info, err)
				}
			}
			if requests != 1 {
				t.Errorf("expected one fixture download, got %d", requests)
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("completed update or restoration orphaned server backups: %v, %v", entries, err)
			}
		})
	}
}

func frameworkConfigArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	entries := make(map[string]string)
	for name, contents := range files {
		name = path.Join("BepInExPack_Valheim", filepath.ToSlash(name))
		entries[name] = contents
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			entries[dir+"/"] = ""
		}
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, entries[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
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
