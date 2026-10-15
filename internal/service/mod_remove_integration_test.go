package service_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestModRemoval_PreservesStateOnFailure(t *testing.T) {
	tests := map[string]struct {
		all           bool
		backupFailure bool
		deleteFailure bool
		missingFiles  bool
	}{
		"given a failed backup, when removing one mod, then preserve files and records": {
			backupFailure: true,
		},
		"given a failed backup, when removing all mods, then preserve files and records": {
			all: true, backupFailure: true,
		},
		"given SQLite rejects deletion, when removing one mod, then preserve files and records": {
			deleteFailure: true,
		},
		"given SQLite rejects deletion, when removing all mods, then preserve files and records": {
			all: true, deleteFailure: true,
		},
		"given installed mods, when removing one mod, then remove only its files and record": {},
		"given installed mods, when removing all mods, then remove files and records": {
			all: true,
		},
		"given missing plugin files, when removing one mod, then remove only its stale record": {
			missingFiles: true,
		},
		"given missing plugin files, when removing all mods, then remove stale records and create an empty directory": {
			all: true, missingFiles: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			db := th.CreateDatabase()
			repo.CreateFrameworksTable(db)
			repo.CreateModsTable(db)
			th.SeedFrameworksTable(repo.NewFrameworksRepo(db))
			mr := repo.NewModsRepo(db)
			mods := []mod.Mod{
				{Name: "Installed", Namespace: "Owned", Version: "1.0.0", FrameworkID: 1},
				{Name: "Other", Namespace: "Owned", Version: "1.0.0", FrameworkID: 1},
			}
			plugins := filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory)
			for i := range mods {
				mods[i].FilePath = filepath.Join(plugins, mods[i].FullName())
				if !tt.missingFiles {
					if err := os.MkdirAll(mods[i].FilePath, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(mods[i].FilePath, "plugin.dll"), []byte(mods[i].Name+" bytes"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := mr.InsertMod(mods[i]); err != nil {
					t.Fatal(err)
				}
				persisted, err := mr.GetMod(mods[i].Name)
				if err != nil {
					t.Fatal(err)
				}
				mods[i] = persisted
			}
			if tt.backupFailure {
				if err := os.Symlink(filepath.Join(plugins, "missing"), filepath.Join(plugins, "b-dangling")); err != nil {
					t.Fatal(err)
				}
			}
			if tt.deleteFailure {
				statement, err := db.Prepare(`CREATE TRIGGER prevent_mod_delete BEFORE DELETE ON mods
					BEGIN SELECT RAISE(ABORT, 'controlled deletion failure'); END`)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := statement.Exec(); err != nil {
					t.Fatal(err)
				}
				if err := statement.Close(); err != nil {
					t.Fatal(err)
				}
			}
			sentinel := filepath.Join(th.GetValheimDirectory(), "server-data")
			if err := os.WriteFile(sentinel, []byte("unrelated server bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			answer := "Y\n"
			if tt.all {
				answer = "YES I AM\n"
			}
			ms := service.NewModService(mr, file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory()), &mock.Thunderstore{}, strings.NewReader(answer))
			var err error
			if tt.all {
				err = ms.RemoveAllMods()
			} else {
				err = ms.RemoveMod(mods[0].Namespace, mods[0].Name)
			}
			failed := tt.backupFailure || tt.deleteFailure
			if failed {
				if !errors.Is(err, service.ErrUnableToRemoveMod) {
					t.Fatalf("expected removal failure, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("legitimate removal failed: %v", err)
			}
			for i, m := range mods {
				retained := failed || (!tt.all && i == 1)
				record, recordErr := mr.GetMod(m.Name)
				if retained {
					if recordErr != nil || record.ID != m.ID || record.FullName() != m.FullName() || record.FilePath != m.FilePath {
						t.Errorf("removal lost or changed the record for %s: %+v, %v", m.Name, record, recordErr)
					}
				} else if !errors.Is(recordErr, repo.ErrModFetchNoResults) {
					t.Errorf("expected %s record to be deleted, got %+v, %v", m.Name, record, recordErr)
				}
				data, readErr := os.ReadFile(filepath.Join(m.FilePath, "plugin.dll"))
				if retained && !tt.missingFiles {
					if readErr != nil || string(data) != m.Name+" bytes" {
						t.Errorf("removal changed %s plugin bytes: %q, %v", m.Name, data, readErr)
					}
				} else if !errors.Is(readErr, os.ErrNotExist) {
					t.Errorf("expected no plugin file for %s, got %q, %v", m.Name, data, readErr)
				}
			}
			if tt.all && !failed {
				if entries, err := os.ReadDir(plugins); err != nil || len(entries) != 0 {
					t.Errorf("expected an empty plugins directory, got %v, %v", entries, err)
				}
			}
			if data, err := os.ReadFile(sentinel); err != nil || string(data) != "unrelated server bytes" {
				t.Errorf("removal changed unrelated data: %q, %v", data, err)
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("removal left temporary snapshots: %v, %v", entries, err)
			}
		})
	}
}
