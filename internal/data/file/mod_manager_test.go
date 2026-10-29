package file_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"github.com/ddoyle2017/warden/internal/api"
	"github.com/ddoyle2017/warden/internal/data/file"
	"github.com/ddoyle2017/warden/internal/test/helper"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	tests := map[string]string{
		"given an absent target, when removing it, then preserve installed mods":   "some-fake-mod-that-is-not-real_0.0.1",
		"given an installed target, when removing it, then finalize only that mod": helper.TestModFullName,
	}
	for name, fullName := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			th.SetUpServerFiles()
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			plugins := filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory)
			payload := filepath.Join(plugins, helper.TestModFullName, "WhereYouAt.dll")
			original, err := os.ReadFile(payload)
			if err != nil {
				t.Fatal(err)
			}
			unrelated := filepath.Join(plugins, "unrelated.dll")
			if err := os.WriteFile(unrelated, []byte("unrelated plugin bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			manager := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())
			if err := manager.RemoveMod(fullName); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(payload)
			if fullName == helper.TestModFullName {
				if !errors.Is(err, os.ErrNotExist) {
					t.Errorf("removed mod remains installed: %v", err)
				}
			} else if err != nil || !bytes.Equal(data, original) {
				t.Errorf("absent-target removal changed installed bytes: %v", err)
			}
			if data, err := os.ReadFile(unrelated); err != nil || string(data) != "unrelated plugin bytes" {
				t.Errorf("removal changed unrelated plugin bytes: %q, %v", data, err)
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("file-only removal did not finalize its snapshot: %v, %v", entries, err)
			}
			if staged, err := filepath.Glob(filepath.Join(filepath.Dir(plugins), ".warden-removal-*")); err != nil || len(staged) != 0 {
				t.Errorf("file-only removal left staging: %v, %v", staged, err)
			}
		})
	}
}

func TestRemoveAllMods_Happy(t *testing.T) {
	tests := map[string]bool{
		"given no installed mods, when removing all, then create an empty plugins directory": false,
		"given installed mods, when removing all, then finalize their removal":               true,
	}
	for name, populated := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			if populated {
				th.SetUpServerFiles()
			}
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			sentinel := filepath.Join(th.GetValheimDirectory(), "server-data")
			if err := os.WriteFile(sentinel, []byte("unrelated server bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			manager := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())
			if err := manager.RemoveAllMods(); err != nil {
				t.Fatal(err)
			}
			plugins := filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory)
			if entries, err := os.ReadDir(plugins); err != nil || len(entries) != 0 {
				t.Errorf("expected an empty plugins directory: %v, %v", entries, err)
			}
			if data, err := os.ReadFile(sentinel); err != nil || string(data) != "unrelated server bytes" {
				t.Errorf("removal changed unrelated server bytes: %q, %v", data, err)
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("file-only removal did not finalize its snapshot: %v, %v", entries, err)
			}
			if staged, err := filepath.Glob(filepath.Join(filepath.Dir(plugins), ".warden-removal-*")); err != nil || len(staged) != 0 {
				t.Errorf("file-only removal left staging: %v, %v", staged, err)
			}
		})
	}
}
