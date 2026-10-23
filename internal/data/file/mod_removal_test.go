package file_test

import (
	"errors"
	"github.com/ddoyle2017/warden/internal/data/file"
	"github.com/ddoyle2017/warden/internal/test/helper"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModRemoval_CommitAndRollback(t *testing.T) {
	tests := map[string]struct {
		all    bool
		commit bool
	}{
		"given a staged mod, when committing, then remove only that mod":            {commit: true},
		"given staged mods, when committing, then leave an empty plugins directory": {all: true, commit: true},
		"given a staged mod, when rolling back, then restore its original bytes":    {},
		"given staged mods, when rolling back, then restore all original bytes":     {all: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			plugins := filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory)
			target := filepath.Join(plugins, helper.TestModFullName)
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
			payload := filepath.Join(target, "plugin.dll")
			other := filepath.Join(plugins, "other.dll")
			for _, path := range []string{payload, other} {
				if err := os.WriteFile(path, []byte("original bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			m := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())
			var removal file.ModRemoval
			var err error
			if tt.all {
				removal, err = m.StageAllModsRemoval()
			} else {
				removal, err = m.StageModRemoval(helper.TestModFullName)
			}
			if err != nil {
				t.Fatal(err)
			}
			live, err := th.GetFileList(plugins)
			if err != nil {
				t.Fatal(err)
			}
			wantLive := 1
			if tt.all {
				wantLive = 0
			}
			if len(live) != wantLive {
				t.Errorf("staged DLLs remain in the live plugin tree: %v", live)
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 1 {
				t.Fatalf("expected one retained snapshot, got %v, %v", entries, err)
			}
			if tt.commit {
				err = removal.Commit()
			} else {
				err = removal.Rollback()
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{payload, other} {
				retained := !tt.commit || (!tt.all && path == other)
				data, err := os.ReadFile(path)
				if retained {
					if err != nil || string(data) != "original bytes" {
						t.Errorf("unexpected retained bytes at %s: %q, %v", path, data, err)
					}
				} else if !errors.Is(err, os.ErrNotExist) {
					t.Errorf("expected removed file %s, got %q, %v", path, data, err)
				}
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("snapshot was not finalized: %v, %v", entries, err)
			}
			if staged, err := filepath.Glob(filepath.Join(filepath.Dir(plugins), ".warden-removal-*")); err != nil || len(staged) != 0 {
				t.Errorf("staging was not finalized: %v, %v", staged, err)
			}
			if err := removal.Rollback(); !errors.Is(err, file.ErrModRemovalFinished) {
				t.Errorf("completed operation remained recoverable: %v", err)
			}
		})
	}
}

func TestModRemoval_RollbackPreservesConflictingFiles(t *testing.T) {
	for _, all := range []bool{false, true} {
		name := "given a staged mod and a replacement file, when rollback fails, then retain both"
		if all {
			name = "given staged mods and a replacement file, when rollback fails, then retain both"
		}
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			t.Setenv("TMPDIR", t.TempDir())
			plugins := filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory)
			modPath := filepath.Join(plugins, helper.TestModFullName)
			if err := os.MkdirAll(modPath, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(modPath, "plugin.dll"), []byte("original bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			m := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())
			var removal file.ModRemoval
			var err error
			target := modPath
			if all {
				removal, err = m.StageAllModsRemoval()
				target = plugins
			} else {
				removal, err = m.StageModRemoval(helper.TestModFullName)
			}
			if err != nil {
				t.Fatal(err)
			}
			if all {
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(target, []byte("new unrelated bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			staged, err := filepath.Glob(filepath.Join(filepath.Dir(plugins), ".warden-removal-*"))
			if err != nil || len(staged) != 1 {
				t.Fatalf("expected one staged directory, got %v, %v", staged, err)
			}
			err = removal.Rollback()
			if !errors.Is(err, file.ErrBackupRestoreFailed) || !strings.Contains(err.Error(), staged[0]) {
				t.Fatalf("expected a recovery error with the retained path, got %v", err)
			}
			if data, err := os.ReadFile(target); err != nil || string(data) != "new unrelated bytes" {
				t.Errorf("rollback destroyed the conflicting file: %q, %v", data, err)
			}
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			if err := removal.Rollback(); err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(filepath.Join(modPath, "plugin.dll")); err != nil || string(data) != "original bytes" {
				t.Errorf("failed rollback lost the retained original: %q, %v", data, err)
			}
		})
	}
}

func TestModRemoval_CleanupFailureCannotRestorePartialFiles(t *testing.T) {
	th := helper.NewHelper(t)
	t.Setenv("TMPDIR", t.TempDir())
	plugins := filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory)
	target := filepath.Join(plugins, helper.TestModFullName)
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"plugin.dll", "remaining.cfg"} {
		if err := os.WriteFile(filepath.Join(target, name), []byte("original bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())
	removal, err := m.StageModRemoval(helper.TestModFullName)
	if err != nil {
		t.Fatal(err)
	}
	cleanupFailure := errors.New("staging cleanup failed")
	var staged string
	file.SetModRemovalCleanup(removal, func(path string) error {
		staged = path
		if err := os.Remove(filepath.Join(path, "contents", "plugin.dll")); err != nil {
			t.Fatal(err)
		}
		return cleanupFailure
	})
	err = removal.Commit()
	var committed *file.ModRemovalCleanupError
	if !errors.As(err, &committed) || !errors.Is(err, cleanupFailure) {
		t.Fatalf("expected committed-removal cleanup diagnostic, got %v", err)
	}
	if !strings.Contains(err.Error(), staged) {
		t.Errorf("cleanup diagnostic omitted the remaining path: %v", err)
	}
	if err := removal.Rollback(); !errors.Is(err, file.ErrModRemovalFinished) {
		t.Errorf("partial cleanup was advertised as recoverable: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("rollback recreated a partially deleted mod: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(staged, "contents", "remaining.cfg")); err != nil || string(data) != "original bytes" {
		t.Errorf("expected the diagnosed cleanup remainder: %q, %v", data, err)
	}
}
