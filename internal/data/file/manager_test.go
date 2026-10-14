package file_test

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/data/file"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestManager_BackupFailure(t *testing.T) {
	tests := map[string]struct {
		modBackup bool
		run       func(file.Manager) (string, error)
		expected  error
	}{
		"given an incomplete backup, when installing a mod, then preserve the server files": {
			modBackup: true,
			run: func(m file.Manager) (string, error) {
				return m.InstallMod(helper.TestDownloadURL, helper.TestModFullName)
			},
			expected: file.ErrBackupCreateFailed,
		},
		"given an incomplete backup, when removing a mod, then preserve the server files": {
			modBackup: true,
			run: func(m file.Manager) (string, error) {
				return "", m.RemoveMod(helper.TestModFullName)
			},
			expected: file.ErrModDeleteFailed,
		},
		"given an incomplete backup, when removing all mods, then preserve the server files": {
			modBackup: true,
			run: func(m file.Manager) (string, error) {
				return "", m.RemoveAllMods()
			},
			expected: file.ErrDeleteAllModsFailed,
		},
		"given an incomplete backup, when installing BepInEx, then preserve the server files": {
			run: func(m file.Manager) (string, error) {
				return m.InstallBepInEx(helper.TestDownloadURL, helper.TestBepInExFullName)
			},
			expected: file.ErrFrameworkInstallFailed,
		},
		"given an incomplete backup, when updating BepInEx, then preserve the server files": {
			run: func(m file.Manager) (string, error) {
				return "", m.UpdateBepInEx(helper.TestDownloadURL, helper.TestBepInExFullName)
			},
			expected: file.ErrFrameworkUpdateFailed,
		},
		"given an incomplete backup, when removing BepInEx, then preserve the server files": {
			run: func(m file.Manager) (string, error) {
				return "", m.RemoveBepInEx()
			},
			expected: file.ErrFrameworkDeleteFailed,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			server := th.GetValheimDirectory()
			files := map[string]string{
				filepath.Join(file.BepInExPluginDirectory, helper.TestModFullName, "plugin.dll"): "mod bytes",
				filepath.Join(file.BepInExPluginDirectory, "z-plugin-data"):                      "unrelated plugin bytes",
				filepath.Join("BepInEx", "core", "runtime.dll"):                                  "framework bytes",
				"z-server-data": "irreplaceable server bytes",
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
			source := server
			if tt.modBackup {
				source = filepath.Join(server, file.BepInExPluginDirectory)
			}
			link := filepath.Join(source, "b-dangling")
			target := filepath.Join(source, "missing")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			before, err := th.GetFileList(server)
			if err != nil {
				t.Fatal(err)
			}
			body := strings.NewReader("invalid ZIP bytes")
			client := &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(body)}, nil
				},
			}

			path, err := tt.run(file.NewManager(client, server))
			if !errors.Is(err, tt.expected) || !errors.Is(err, file.ErrBackupCreateFailed) {
				t.Errorf("expected %v wrapping a backup failure, got %v", tt.expected, err)
			}
			if path != "" {
				t.Errorf("failed installation returned path %q", path)
			}
			if body.Len() != len("invalid ZIP bytes") {
				t.Error("operation consumed the download after backup creation failed")
			}
			for name, contents := range files {
				data, err := os.ReadFile(filepath.Join(server, name))
				if err != nil || string(data) != contents {
					t.Errorf("operation changed %s: %q, %v", name, data, err)
				}
			}
			if got, err := os.Readlink(link); err != nil || got != target {
				t.Errorf("operation changed the dangling symlink: %q, %v", got, err)
			}
			after, err := th.GetFileList(server)
			if err != nil {
				t.Fatal(err)
			}
			if !th.AreFileListsEqual(before, after) {
				t.Error("operation added or removed server files")
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("incomplete backups remain: %v, %v", entries, err)
			}
		})
	}
}
