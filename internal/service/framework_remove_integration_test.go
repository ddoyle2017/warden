package service_test

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestFrameworkRemovalFilesAndRecords(t *testing.T) {
	t.Run("given associated and legacy plugins, when removing BepInEx, then remove only records for the removed tree", func(t *testing.T) {
		th := helper.NewHelper(t)
		backups := t.TempDir()
		t.Setenv("TMPDIR", backups)
		server := th.GetValheimDirectory()
		db := th.CreateDatabase().(*sql.DB)
		repo.CreateFrameworksTable(db)
		repo.CreateModsTable(db)
		fr, mr := repo.NewFrameworksRepo(db), repo.NewModsRepo(db)
		for _, f := range []framework.Framework{
			{Name: framework.BepInEx, Namespace: framework.BepInExNamespace, Version: "1.0.0"},
			{Name: "OtherFramework", Namespace: "Other", Version: "1.0.0"},
		} {
			if err := fr.InsertFramework(f); err != nil {
				t.Fatal(err)
			}
		}
		current, err := fr.GetFramework(framework.BepInEx)
		if err != nil {
			t.Fatal(err)
		}
		other, err := fr.GetFramework("OtherFramework")
		if err != nil {
			t.Fatal(err)
		}
		mods := []struct {
			record  mod.Mod
			removed bool
		}{
			{record: mod.Mod{Name: "Associated", FrameworkID: current.ID}, removed: true},
			{record: mod.Mod{Name: "Legacy"}, removed: true},
			{record: mod.Mod{Name: "StaleAssociation", FrameworkID: other.ID}, removed: true},
			{record: mod.Mod{Name: "OtherFrameworkPlugin", FrameworkID: other.ID}},
			{record: mod.Mod{Name: "LegacyOutside"}},
		}
		for i := range mods {
			m := &mods[i].record
			m.Namespace, m.Version = "Owned", "1.0.0"
			root := filepath.Join(server, "BepInEx", "plugins")
			if !mods[i].removed {
				root = filepath.Join(server, "other-plugins")
			}
			m.FilePath = filepath.Join(root, m.FullName())
			if err := os.MkdirAll(m.FilePath, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(m.FilePath, "plugin.dll"), []byte(m.Name+" bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := mr.InsertMod(*m); err != nil {
				t.Fatal(err)
			}
			*m, err = mr.GetMod(m.Name)
			if err != nil {
				t.Fatal(err)
			}
		}
		for name, contents := range map[string]string{
			"start_server_bepinex.sh": "framework startup",
			"world.db":                "unrelated world",
		} {
			if err := os.WriteFile(filepath.Join(server, name), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		db.SetMaxOpenConns(1)
		if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
			t.Fatal(err)
		}

		manager := file.NewManager(&mock.HTTPClient{}, server)
		svc := service.NewFrameworkService(fr, manager, &mock.Thunderstore{}, strings.NewReader("YES I AM\n"))
		if err := svc.RemoveBepInEx(); err != nil {
			t.Fatal(err)
		}

		for _, tt := range mods {
			actual, err := mr.GetMod(tt.record.Name)
			if tt.removed {
				if !errors.Is(err, repo.ErrModFetchNoResults) {
					t.Errorf("removed plugin %s is still recorded: %+v, %v", tt.record.Name, actual, err)
				}
			} else if err != nil || !actual.Equals(&tt.record) {
				t.Errorf("unrelated record changed: %+v, %v; want %+v", actual, err, tt.record)
			}
			data, err := os.ReadFile(filepath.Join(tt.record.FilePath, "plugin.dll"))
			if tt.removed {
				if !errors.Is(err, os.ErrNotExist) {
					t.Errorf("removed plugin still exists: %q, %v", data, err)
				}
			} else if err != nil || string(data) != tt.record.Name+" bytes" {
				t.Errorf("unrelated plugin changed: %q, %v", data, err)
			}
		}
		if _, err := fr.GetFramework(framework.BepInEx); !errors.Is(err, repo.ErrFrameworkFetchNoResults) {
			t.Errorf("removed framework still recorded: %v", err)
		}
		if actual, err := fr.GetFramework(other.Name); err != nil || actual != other {
			t.Errorf("unrelated framework changed: %+v, %v; want %+v", actual, err, other)
		}
		for _, name := range []string{"BepInEx", "start_server_bepinex.sh"} {
			if _, err := os.Lstat(filepath.Join(server, name)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("framework artifact %q remains: %v", name, err)
			}
		}
		if data, err := os.ReadFile(filepath.Join(server, "world.db")); err != nil || string(data) != "unrelated world" {
			t.Errorf("unrelated world changed: %q, %v", data, err)
		}
		if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
			t.Errorf("removal retained temporary backups: %v, %v", entries, err)
		}
	})
}

func TestFrameworkRemovalServiceRecordedMods(t *testing.T) {
	for name, tt := range map[string]struct{ frameworkPresent, filesAlreadyRemoved bool }{
		"given a newly installed mod, when removing its framework, then remove the service-created record": {
			frameworkPresent: true,
		},
		"given plugins without a framework record, when removing BepInEx, then remove their records": {},
		"given previously removed files, when retrying framework removal, then clear the stale records": {
			filesAlreadyRemoved: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t, helper.WithDataFolder("../test/data"))
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			db := th.CreateDatabase()
			repo.CreateFrameworksTable(db)
			repo.CreateModsTable(db)
			fr, mr := repo.NewFrameworksRepo(db), repo.NewModsRepo(db)
			if tt.frameworkPresent {
				th.SeedFrameworksTable(fr)
			}
			release := thunderstore.Release{
				Name: "Where_You_At", Namespace: "Azumatt", VersionNumber: "1.0.9",
				FullName: helper.TestModFullName, DownloadURL: helper.TestDownloadURL,
			}
			requests := 0
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				requests++
				if url != release.DownloadURL {
					t.Fatalf("unexpected download URL %q", url)
				}
				archive, err := os.Open(filepath.Join(th.GetDataDirectory(), helper.TestModFullName+file.ZipFileExtension))
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{Body: archive}, nil
			}}
			ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
				if namespace != release.Namespace || name != release.Name {
					t.Fatalf("unexpected package %q/%q", namespace, name)
				}
				return thunderstore.Package{Latest: release}, nil
			}}
			manager := file.NewManager(client, th.GetValheimDirectory())
			ms := service.NewModService(mr, manager, ts, strings.NewReader(""))
			if err := ms.AddMod(release.Namespace, release.Name); err != nil {
				t.Fatal(err)
			}
			installed, err := mr.GetMod(release.Name)
			if err != nil {
				t.Fatal(err)
			}
			if files, err := th.GetFileList(installed.FilePath); err != nil || len(files) == 0 {
				t.Fatalf("installation did not create plugin files: %v, %v", files, err)
			}
			if tt.filesAlreadyRemoved {
				if err := manager.RemoveBepInEx(); err != nil {
					t.Fatal(err)
				}
				if actual, err := mr.GetMod(release.Name); err != nil || !actual.Equals(&installed) {
					t.Fatalf("file-only removal unexpectedly changed metadata: %+v, %v", actual, err)
				}
			}

			svc := service.NewFrameworkService(fr, manager, &mock.Thunderstore{}, strings.NewReader("YES I AM\n"))
			if err := svc.RemoveBepInEx(); err != nil {
				t.Fatal(err)
			}
			if mods, err := ms.ListMods(); err != nil || len(mods) != 0 {
				t.Errorf("removed plugin still listed: %+v, %v", mods, err)
			}
			if _, err := fr.GetFramework(framework.BepInEx); !errors.Is(err, repo.ErrFrameworkFetchNoResults) {
				t.Errorf("removed framework still recorded: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(th.GetValheimDirectory(), "BepInEx")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("plugin tree remains: %v", err)
			}
			if requests != 1 {
				t.Errorf("download requests = %d; want only the original installation", requests)
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("removal retained temporary snapshots: %v, %v", entries, err)
			}
		})
	}
}
