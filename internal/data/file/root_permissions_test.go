package file_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/data/file"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestLiveRootPermissions(t *testing.T) {
	mask := os.Getenv("WARDEN_TEST_LIVE_ROOT_UMASK")
	if mask == "" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		for _, mask := range []string{"0022", "0077"} {
			t.Run("given umask "+mask+", when replacing live roots, then preserve their permissions", func(t *testing.T) {
				// Set the umask only in a child, never in the shared test process.
				cmd := exec.Command("sh", "-c", `umask "$1" && shift && exec "$@"`, "sh", mask,
					executable, "-test.run=^TestLiveRootPermissions$", "-test.count=1", "-test.v")
				cmd.Env = append(os.Environ(), "WARDEN_TEST_LIVE_ROOT_UMASK="+mask, "HOME="+t.TempDir())
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("live-root permissions with umask %s: %v\n%s", mask, err, output)
				}
			})
		}
		return
	}

	defaultModes := map[string]os.FileMode{"0022": 0o755, "0077": 0o700}
	defaultMode, ok := defaultModes[mask]
	if !ok {
		t.Fatalf("unexpected child umask %q", mask)
	}
	control := filepath.Join(t.TempDir(), "control")
	if err := os.Mkdir(control, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	assertPluginDirectoryModes(t, control, map[string]os.FileMode{".": defaultMode})

	for _, mode := range []os.FileMode{0o700, 0o775, 0o755} {
		for _, operation := range []string{"restore", "restore missing root", "restore missing root after failed backup", "remove all mods", "roll back all mods"} {
			name := fmt.Sprintf("given a %04o live root, when %s, then preserve its mode and expected files", mode, operation)
			t.Run(name, func(t *testing.T) {
				th := helper.NewHelper(t)
				server, backups := th.GetValheimDirectory(), t.TempDir()
				t.Setenv("TMPDIR", backups)
				plugins := filepath.Join(server, file.BepInExPluginDirectory)
				saved := map[string]string{
					"plugin.dll":        "original plugin\x00bytes",
					"nested/config.cfg": "original settings",
				}
				for name, contents := range saved {
					path := filepath.Join(plugins, name)
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				sentinel := filepath.Join(server, "server-data")
				if err := os.WriteFile(sentinel, []byte("unrelated server bytes"), 0o600); err != nil {
					t.Fatal(err)
				}

				var backup file.Backup
				missingRoot := operation == "restore missing root" || operation == "restore missing root after failed backup"
				if operation == "restore" || missingRoot {
					sourceMode := os.FileMode(0o750)
					if missingRoot {
						sourceMode = mode
					}
					if err := os.Chmod(plugins, sourceMode); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(filepath.Join(plugins, "plugin.dll"), 0o644); err != nil {
						t.Fatal(err)
					}
					backup = file.NewBackup()
					if err := backup.Create(plugins); err != nil {
						t.Fatal(err)
					}
					assertPluginDirectoryModes(t, *backup.Path(), map[string]os.FileMode{".": 0o700})
					assertPluginFiles(t, th, *backup.Path(), saved)
					if operation == "restore missing root after failed backup" {
						prior := *backup.Path()
						rejected := filepath.Join(server, "rejected-source")
						rejectedMode := os.FileMode(0o775)
						if mode == rejectedMode {
							rejectedMode = 0o700
						}
						if err := os.Mkdir(rejected, 0o700); err != nil {
							t.Fatal(err)
						}
						if err := os.Chmod(rejected, rejectedMode); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(rejected, "a-copied"), []byte("rejected replacement bytes"), 0o600); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(filepath.Join(rejected, "missing"), filepath.Join(rejected, "b-dangling")); err != nil {
							t.Fatal(err)
						}
						if err := backup.Create(rejected); !errors.Is(err, file.ErrBackupCreateFailed) || !errors.Is(err, file.ErrFileOpenFailed) {
							t.Fatalf("expected the replacement backup copy to fail, got %v", err)
						}
						if backup.Path() == nil || *backup.Path() != prior {
							t.Fatal("failed creation replaced the active backup")
						}
						assertPluginDirectoryModes(t, rejected, map[string]os.FileMode{".": rejectedMode})
						assertFileContents(t, filepath.Join(rejected, "a-copied"), "rejected replacement bytes")
						assertPluginDirectoryModes(t, prior, map[string]os.FileMode{".": 0o700})
						assertPluginFiles(t, th, prior, saved)
						if entries, err := os.ReadDir(backups); err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(prior) {
							t.Fatalf("failed replacement did not retain only the original backup: %v, %v", entries, err)
						}
					}
					for _, name := range []string{"plugin.dll", "live-only"} {
						if err := os.WriteFile(filepath.Join(plugins, name), []byte("changed live bytes"), 0o600); err != nil {
							t.Fatal(err)
						}
					}
				}
				if err := os.Chmod(plugins, mode); err != nil {
					t.Fatal(err)
				}
				assertPluginDirectoryModes(t, plugins, map[string]os.FileMode{".": mode})
				if missingRoot {
					if err := os.RemoveAll(plugins); err != nil {
						t.Fatal(err)
					}
				}

				manager := file.NewManager(&mock.HTTPClient{}, server)
				switch operation {
				case "restore", "restore missing root", "restore missing root after failed backup":
					if err := backup.Restore(plugins); err != nil {
						t.Fatal(err)
					}
					if backup.Path() != nil {
						t.Error("successful restoration retained an active backup")
					}
					assertPluginFiles(t, th, plugins, saved)
					if info, err := os.Stat(filepath.Join(plugins, "plugin.dll")); err != nil || info.Mode().Perm() != 0o644 {
						t.Errorf("restoration changed the sentinel's permissions: %v, %v", info, err)
					}
				case "remove all mods":
					if err := manager.RemoveAllMods(); err != nil {
						t.Fatal(err)
					}
					if entries, err := os.ReadDir(plugins); err != nil || len(entries) != 0 {
						t.Errorf("expected an empty plugins directory, got %v, %v", entries, err)
					}
				case "roll back all mods":
					removal, err := manager.StageAllModsRemoval()
					if err != nil {
						t.Fatal(err)
					}
					assertPluginDirectoryModes(t, plugins, map[string]os.FileMode{".": mode})
					if entries, err := os.ReadDir(plugins); err != nil || len(entries) != 0 {
						t.Fatalf("expected an empty plugins directory before rollback, got %v, %v", entries, err)
					}
					staged, err := filepath.Glob(filepath.Join(filepath.Dir(plugins), ".warden-removal-*"))
					if err != nil || len(staged) != 1 {
						t.Fatalf("expected one retained staging directory, got %v, %v", staged, err)
					}
					snapshots, err := os.ReadDir(backups)
					if err != nil || len(snapshots) != 1 {
						t.Fatalf("expected one retained backup, got %v, %v", snapshots, err)
					}
					snapshot := filepath.Join(backups, snapshots[0].Name())
					contents := filepath.Join(staged[0], "contents")
					assertPluginDirectoryModes(t, staged[0], map[string]os.FileMode{".": 0o700, "contents": mode})
					assertPluginDirectoryModes(t, snapshot, map[string]os.FileMode{".": 0o700})
					assertPluginFiles(t, th, contents, saved)
					assertPluginFiles(t, th, snapshot, saved)

					conflict := filepath.Join(plugins, "new-plugin", "nested", "sentinel.dll")
					if err := os.MkdirAll(filepath.Dir(conflict), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(conflict, []byte("new unrelated bytes"), 0o600); err != nil {
						t.Fatal(err)
					}
					err = removal.Rollback()
					if !errors.Is(err, file.ErrBackupRestoreFailed) || !strings.Contains(err.Error(), staged[0]) {
						t.Fatalf("expected rollback refusal with the retained staging path, got %v", err)
					}
					assertPluginFiles(t, th, plugins, map[string]string{"new-plugin/nested/sentinel.dll": "new unrelated bytes"})
					assertPluginDirectoryModes(t, plugins, map[string]os.FileMode{".": mode})
					assertPluginFiles(t, th, contents, saved)
					assertPluginFiles(t, th, snapshot, saved)
					assertPluginDirectoryModes(t, staged[0], map[string]os.FileMode{".": 0o700, "contents": mode})
					assertPluginDirectoryModes(t, snapshot, map[string]os.FileMode{".": 0o700})

					for _, path := range []string{conflict, filepath.Dir(conflict), filepath.Dir(filepath.Dir(conflict))} {
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
					}
					if err := removal.Rollback(); err != nil {
						t.Fatalf("retry after explicitly removing the conflict failed: %v", err)
					}
					assertPluginFiles(t, th, plugins, saved)
					if err := removal.Rollback(); !errors.Is(err, file.ErrModRemovalFinished) {
						t.Errorf("successful rollback left an active removal: %v", err)
					}
				default:
					t.Fatalf("unexpected operation %q", operation)
				}

				assertPluginDirectoryModes(t, plugins, map[string]os.FileMode{".": mode})
				assertFileContents(t, sentinel, "unrelated server bytes")
				if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
					t.Errorf("operation left backup artifacts: %v, %v", entries, err)
				}
				if entries, err := os.ReadDir(filepath.Dir(plugins)); err != nil || len(entries) != 1 || entries[0].Name() != "plugins" {
					t.Errorf("operation left staging artifacts: %v, %v", entries, err)
				}
			})
		}
	}
}
