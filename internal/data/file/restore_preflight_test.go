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

func TestRestorePreflight(t *testing.T) {
	installFramework := func(m file.Manager) (string, error) {
		return m.InstallBepInEx(helper.TestDownloadURL, helper.TestBepInExFullName)
	}
	installMod := func(m file.Manager) (string, error) {
		return m.InstallMod(helper.TestDownloadURL, helper.TestModFullName)
	}
	tests := map[string]struct {
		workingDirectory string
		run              func(file.Manager) (string, error)
		wantError        error
		downloads        int
	}{
		"given cwd inside the server, when installing a framework, then refuse before mutation": {
			workingDirectory: "server", run: installFramework, wantError: file.ErrFrameworkInstallFailed,
		},
		"given cwd below the server, when installing a framework, then refuse before mutation": {
			workingDirectory: "plugins", run: installFramework, wantError: file.ErrFrameworkInstallFailed,
		},
		"given cwd outside the server, when an archive is invalid, then restore the original tree": {
			run: installFramework, wantError: file.ErrZipReadFailed, downloads: 1,
		},
		"given cwd inside the server, when updating the framework, then refuse before staging": {
			workingDirectory: "server",
			run: func(m file.Manager) (string, error) {
				return "", m.UpdateBepInEx(helper.TestDownloadURL, helper.TestBepInExFullName)
			},
			wantError: file.ErrFrameworkUpdateFailed,
		},
		"given cwd inside the server, when removing the framework, then refuse before deletion": {
			workingDirectory: "server",
			run:              func(m file.Manager) (string, error) { return "", m.RemoveBepInEx() },
			wantError:        file.ErrFrameworkDeleteFailed,
		},
		"given cwd inside plugins, when installing a mod, then refuse before mutation": {
			workingDirectory: "plugins", run: installMod, wantError: file.ErrBackupRestoreFailed,
		},
		"given cwd outside plugins but inside the server, when a mod archive is invalid, then restore plugins": {
			workingDirectory: "server", run: installMod, wantError: file.ErrZipReadFailed, downloads: 1,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			server, backups := th.GetValheimDirectory(), t.TempDir()
			t.Setenv("TMPDIR", backups)
			files := map[string]string{
				"server-data": "unrelated bytes", "BepInEx/core/runtime": "framework bytes",
				"BepInEx/plugins/installed/plugin.dll": "plugin bytes",
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
			if tt.workingDirectory != "" {
				cwd := server
				if tt.workingDirectory == "plugins" {
					cwd = filepath.Join(server, file.BepInExPluginDirectory)
				}
				useWorkingDirectory(t, cwd)
			}
			downloads := 0
			client := &mock.HTTPClient{GetFunc: func(string) (*http.Response, error) {
				downloads++
				return &http.Response{Body: io.NopCloser(strings.NewReader("invalid zip bytes"))}, nil
			}}
			path, err := tt.run(file.NewManager(client, server))
			if !errors.Is(err, tt.wantError) || path != "" {
				t.Errorf("expected %v and no installation path, got %v, %q", tt.wantError, err, path)
			}
			if tt.downloads == 0 && (!errors.Is(err, file.ErrBackupRestoreFailed) || !strings.Contains(err.Error(), server)) {
				t.Errorf("preflight omitted the restore refusal or destination: %v", err)
			}
			if downloads != tt.downloads {
				t.Errorf("download calls = %d, want %d", downloads, tt.downloads)
			}
			after, err := th.GetFileList(server)
			if err != nil {
				t.Fatal(err)
			}
			if !th.AreFileListsEqual(files, after) {
				t.Errorf("live file set changed: %v", after)
			}
			for name, contents := range files {
				if data, err := os.ReadFile(filepath.Join(server, name)); err != nil || string(data) != contents {
					t.Errorf("live file %s changed: %q, %v", name, data, err)
				}
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("operation left snapshots behind: %v, %v", entries, err)
			}
		})
	}
}

func TestStagedRemovalFromPluginsWorkingDirectory(t *testing.T) {
	for _, all := range []bool{false, true} {
		name := "given cwd inside plugins, when removing one mod, then rename-back recovery remains available"
		if all {
			name = "given cwd inside plugins, when removing all mods, then rename-back recovery remains available"
		}
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			t.Setenv("TMPDIR", t.TempDir())
			plugins := filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory)
			plugin := filepath.Join(plugins, helper.TestModFullName, "plugin.dll")
			if err := os.MkdirAll(filepath.Dir(plugin), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(plugin, []byte("original plugin bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := th.GetFileList(plugins)
			if err != nil {
				t.Fatal(err)
			}
			useWorkingDirectory(t, plugins)
			m := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())
			var removal file.ModRemoval
			if all {
				removal, err = m.StageAllModsRemoval()
			} else {
				removal, err = m.StageModRemoval(helper.TestModFullName)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(plugin); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("original plugin was not staged: %v", err)
			}
			if err := removal.Rollback(); err != nil {
				t.Fatal(err)
			}
			after, err := th.GetFileList(plugins)
			if err != nil || !th.AreFileListsEqual(before, after) {
				t.Fatalf("rename-back recovery changed the file set: %v, %v", after, err)
			}
			if data, err := os.ReadFile(plugin); err != nil || string(data) != "original plugin bytes" {
				t.Errorf("rename-back recovery changed plugin bytes: %v", err)
			}
		})
	}
}

func useWorkingDirectory(t *testing.T, path string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore test working directory: %v", err)
		}
	})
}
