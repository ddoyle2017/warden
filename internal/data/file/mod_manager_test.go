package file_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/api"
	"warden/internal/data/file"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestInstallMod_Happy(t *testing.T) {
	tests := map[string]bool{
		"given no plugins directory, when installing the first mod, then create it and install the files": false,
		"given an empty plugins directory, when installing the first mod, then install the files":         true,
	}

	for name, createDirectory := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			t.Setenv("TMPDIR", t.TempDir())
			modDir := filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory)
			if createDirectory {
				if err := os.MkdirAll(modDir, os.ModePerm); err != nil {
					t.Fatal(err)
				}
			}
			sentinel := filepath.Join(th.GetValheimDirectory(), "server-data")
			if err := os.WriteFile(sentinel, []byte("unrelated server bytes"), 0o600); err != nil {
				t.Fatal(err)
			}

			var archive bytes.Buffer
			writer := zip.NewWriter(&archive)
			entry, err := writer.Create("plugin.dll")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write([]byte("new plugin\x00bytes")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			client := &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(&archive)}, nil
				},
			}

			manager := file.NewManager(client, th.GetValheimDirectory())
			path, err := manager.InstallMod(helper.TestDownloadURL, helper.TestModFullName)
			if err != nil {
				t.Fatal(err)
			}
			if path != filepath.Join(modDir, helper.TestModFullName) {
				t.Fatalf("unexpected installation path %q", path)
			}
			if data, err := os.ReadFile(filepath.Join(path, "plugin.dll")); err != nil || string(data) != "new plugin\x00bytes" {
				t.Errorf("installed bytes do not match the archive: %q, %v", data, err)
			}
			if data, err := os.ReadFile(sentinel); err != nil || string(data) != "unrelated server bytes" {
				t.Errorf("installation changed unrelated files: %q, %v", data, err)
			}
		})
	}
}

func TestModRemovals_AbsentDirectory(t *testing.T) {
	tests := map[string]struct {
		run          func(file.Manager) error
		wantEmptyDir bool
	}{
		"given no plugins directory, when removing a mod, then leave the server unchanged": {
			run: func(m file.Manager) error { return m.RemoveMod(helper.TestModFullName) },
		},
		"given no plugins directory, when removing all mods, then create an empty plugins directory": {
			run:          func(m file.Manager) error { return m.RemoveAllMods() },
			wantEmptyDir: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			t.Setenv("TMPDIR", t.TempDir())
			sentinel := filepath.Join(th.GetValheimDirectory(), "server-data")
			if err := os.WriteFile(sentinel, []byte("unrelated server bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			m := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())
			if err := tt.run(m); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory))
			if tt.wantEmptyDir {
				if err != nil || len(entries) != 0 {
					t.Errorf("expected an empty plugins directory, got %v, %v", entries, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Errorf("expected plugins directory to remain absent, got %v", err)
			}
			if data, err := os.ReadFile(sentinel); err != nil || string(data) != "unrelated server bytes" {
				t.Errorf("removal changed unrelated files: %q, %v", data, err)
			}
		})
	}
}

func TestInstallMod_Sad(t *testing.T) {
	th := helper.NewHelper(t)

	tests := map[string]struct {
		fullName    string
		client      api.HTTPClient
		expectedErr error
	}{
		"download from URL fails": {
			fullName: helper.TestModFullName,
			client: &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{}, http.ErrContentLength
				},
			},
			expectedErr: api.ErrHTTPClient,
		},
		"failed to create zip file": {
			fullName: "dba\\.. dwanu^&%* d//]\\",
			client: &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("garbagedata")),
					}, nil
				},
			},
			expectedErr: file.ErrFileCreateFailed,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			manager := file.NewManager(tt.client, th.GetValheimDirectory())

			path, err := manager.InstallMod(helper.TestDownloadURL, tt.fullName)
			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected error: %+v, received error: %+v", tt.expectedErr, err)
			}
			if path != "" {
				t.Errorf("expected an empty mod folder path, received: %s", path)
			}

			t.Cleanup(func() {
				th.RemoveServerFiles()
			})
		})
	}
}

func TestRemoveMod_Happy(t *testing.T) {
	th := helper.NewHelper(t)
	th.SetUpServerFiles()

	tests := map[string]struct {
		name string
	}{
		"if mod does not exist, delete nothing and return successful": {
			name: "some-fake-mod-that-is-not-real_0.0.1",
		},
		"successfully removes target mod": {
			name: helper.TestModFullName,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			manager := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())

			err := manager.RemoveMod(test.name)
			if err != nil {
				t.Errorf("expected nil error, received: %+v", err)
			}
		})
	}
	t.Cleanup(func() {
		th.RemoveServerFiles()
	})
}

func TestRemoveAllMods_Happy(t *testing.T) {
	th := helper.NewHelper(t)

	tests := map[string]struct {
		setUp func(t *testing.T)
	}{
		"if no mods are installed, clean directory and return successful": {
			setUp: func(_ *testing.T) {},
		},
		"successfully removes all mods": {
			setUp: func(t *testing.T) {
				th.SetUpServerFiles()
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			test.setUp(t)
			manager := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())

			err := manager.RemoveAllMods()
			if err != nil {
				t.Errorf("expected nil error, received: %+v", err)
			}
		})
	}
	t.Cleanup(func() {
		th.RemoveServerFiles()
	})
}
