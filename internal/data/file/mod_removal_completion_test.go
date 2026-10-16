package file

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileRemovalDirectCompletion(t *testing.T) {
	for _, all := range []bool{false, true} {
		operation := "one mod"
		if all {
			operation = "all mods"
		}
		t.Run("given partial final cleanup, when removing "+operation+", then return direct removal completion with its cause", func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			server := filepath.Join(restoreTempDir(t), "server")
			m := NewManager(nil, server).(*manager)
			const fullName = "Owner-Plugin-1.0.0"
			writeRestoreTree(t, m.modDirectory, map[string]string{
				fullName + "/plugin.dll": "removed plugin",
				fullName + "/config":     "retained cleanup bytes",
				"unrelated.dll":          "unrelated plugin",
			})
			cause := errors.New("staging cleanup interrupted")
			var staged string
			commit := func(removal ModRemoval) error {
				r := removal.(*modRemoval)
				staged = r.staging
				r.removeStaged = func(path string) error {
					target := filepath.Join(path, "contents")
					if all {
						target = filepath.Join(target, fullName)
					}
					if err := os.Remove(filepath.Join(target, "plugin.dll")); err != nil {
						t.Fatal(err)
					}
					return cause
				}
				err := r.Commit()
				if r.backup.Path() != nil || !errors.Is(r.Rollback(), ErrModRemovalFinished) {
					t.Fatal("committed removal retained retryable recovery state")
				}
				return err
			}
			var err error
			sentinel := ErrModDeleteFailed
			if all {
				err, sentinel = m.removeAllMods(commit), ErrDeleteAllModsFailed
			} else {
				err = m.removeMod(fullName, commit)
			}
			if _, completed := err.(*ModRemovalCleanupError); !completed ||
				!errors.Is(err, sentinel) || !errors.Is(err, cause) ||
				staged == "" || !strings.Contains(err.Error(), staged) {
				t.Fatalf("file-only removal outcome lost: %v", err)
			}
			wantLive := map[string]string{}
			wantStaged := map[string]string{"config": "retained cleanup bytes"}
			if all {
				wantStaged = map[string]string{fullName + "/config": "retained cleanup bytes", "unrelated.dll": "unrelated plugin"}
			} else {
				wantLive["unrelated.dll"] = "unrelated plugin"
			}
			assertRestoreTree(t, m.modDirectory, wantLive)
			assertRestoreTree(t, filepath.Join(staged, "contents"), wantStaged)
		})
	}
}
