package service_test

import (
	"bytes"
	"cmp"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestBulkUpdate_DuplicateDependencyStopsBeforeFetch(t *testing.T) {
	for name, duplicate := range map[string]bool{
		"given a unique selected dependency, when Earlier updates it, then finish every selected package":                            false,
		"given duplicate selected dependency records, when Earlier needs it, then retain Earlier progress and stop before Dep fetch": true,
	} {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			server := th.GetValheimDirectory()
			db := th.CreateDatabase()
			repo.CreateFrameworksTable(db)
			repo.CreateModsTable(db)
			th.SeedFrameworksTable(repo.NewFrameworksRepo(db))
			stored := repo.NewModsRepo(db)
			plugins := filepath.Join(server, "BepInEx", "plugins")
			originals := []mod.Mod{
				{Namespace: "Owned", Name: "Earlier", Version: "1.0.0", FrameworkID: 1},
				{Namespace: "Owned", Name: "Dep", Version: "1.0.0", FrameworkID: 1},
			}
			if duplicate {
				originals = append(originals, mod.Mod{Namespace: "Owned", Name: "Dep", Version: "1.1.0", FrameworkID: 1})
			}
			originals = append(originals, mod.Mod{Namespace: "Owned", Name: "Later", Version: "1.0.0", FrameworkID: 1})
			wantFiles := map[string]string{"server-data": "unrelated server data"}
			for i := range originals {
				m := &originals[i]
				m.FilePath = filepath.Join(plugins, m.FullName())
				m.WebsiteURL = "https://example.invalid/original/" + m.FullName()
				m.Description = "original metadata " + m.FullName()
				if err := stored.InsertMod(*m); err != nil {
					t.Fatal(err)
				}
				wantFiles[filepath.Join("BepInEx", "plugins", m.FullName(), "plugin.dll")] = "original DLL " + m.FullName()
				wantFiles[filepath.Join("BepInEx", "plugins", m.FullName(), "settings")] = "original settings " + m.FullName()
			}
			var err error
			originals, err = stored.ListMods()
			if err != nil {
				t.Fatal(err)
			}
			slices.SortFunc(originals, func(a, b mod.Mod) int { return cmp.Compare(a.ID, b.ID) })
			for relative, contents := range wantFiles {
				path := filepath.Join(server, relative)
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			releases := map[string]thunderstore.Release{
				"Earlier": {
					Namespace: "Owned", Name: "Earlier", VersionNumber: "2.0.0", FullName: "Owned-Earlier-2.0.0",
					DownloadURL: "https://example.invalid/Earlier", Description: "updated Earlier",
					Dependencies: []string{"Owned-Dep-1.0.0"},
				},
				"Dep": {
					Namespace: "Owned", Name: "Dep", VersionNumber: "2.0.0", FullName: "Owned-Dep-2.0.0",
					DownloadURL: "https://example.invalid/Dep", Description: "updated Dep",
				},
				"Later": {
					Namespace: "Owned", Name: "Later", VersionNumber: "2.0.0", FullName: "Owned-Later-2.0.0",
					DownloadURL: "https://example.invalid/Later", Description: "updated Later",
				},
			}
			var fetched, downloaded, replacedFrom, replacedTo, written, deleted []string
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				for name, release := range releases {
					if url == release.DownloadURL {
						downloaded = append(downloaded, name)
						return &http.Response{Body: io.NopCloser(bytes.NewReader(modIdentityArchive(t, "updated DLL "+name)))}, nil
					}
				}
				t.Fatalf("unexpected download %q", url)
				return nil, nil
			}}
			manager := file.NewManager(client, server)
			files := &mock.Manager{
				ValidateModInstallFunc: manager.ValidateModInstall,
				ReplaceModFunc: func(url, oldName, newName string) (file.ModReplacement, error) {
					replacedFrom = append(replacedFrom, oldName)
					replacedTo = append(replacedTo, newName)
					return manager.ReplaceMod(url, oldName, newName)
				},
				InstallModFunc: func(url, fullName string) (string, error) {
					t.Errorf("selected dependency unexpectedly used ordinary installation: %s", fullName)
					return manager.InstallMod(url, fullName)
				},
			}
			records := &mock.ModsRepo{
				// This order permits Earlier's dependency work before Dep's selected turn.
				ListModsFunc: func() ([]mod.Mod, error) { return slices.Clone(originals), nil },
				GetModFunc:   stored.GetMod, GetModByIdentityFunc: stored.GetModByIdentity,
				UpsertModFunc: func(m mod.Mod) error {
					written = append(written, m.Name)
					return stored.UpsertMod(m)
				},
				DeleteModFunc: func(name, namespace string) error {
					deleted = append(deleted, namespace+"/"+name)
					return stored.DeleteMod(name, namespace)
				},
			}
			catalog := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
				fetched = append(fetched, namespace+"/"+name)
				release, ok := releases[name]
				if namespace != "Owned" || !ok {
					t.Fatalf("unexpected package %q/%q", namespace, name)
				}
				return thunderstore.Package{Latest: release}, nil
			}}
			result := service.NewModService(records, files, catalog, strings.NewReader("Y\n")).UpdateAllMods()
			t.Logf("result=%v; fetched=%v; downloaded=%v; replaced=%v -> %v; written=%v; deleted=%v",
				result, fetched, downloaded, replacedFrom, replacedTo, written, deleted)
			if duplicate {
				for _, want := range []error{service.ErrUnableToUpdateMod, service.ErrDuplicateModRecords, repo.ErrModFetchMultipleResults} {
					if !errors.Is(result, want) {
						t.Errorf("duplicate-identity refusal lost %v: %v", want, result)
					}
				}
				for _, detail := range []string{"Owned/Dep", "inspect installation records"} {
					if result == nil || !strings.Contains(result.Error(), detail) {
						t.Errorf("duplicate refusal omits %q: %v", detail, result)
					}
				}
				if _, completed := result.(*service.ModMutationCleanupError); completed {
					t.Errorf("duplicate refusal reported whole-request completion: %v", result)
				}
			} else if result != nil {
				t.Errorf("valid unique-dependency control failed: %v", result)
			}
			wantNames := []string{"Earlier"}
			wantFrom := []string{"Owned-Earlier-1.0.0"}
			wantTo := []string{"Owned-Earlier-2.0.0"}
			wantFetches := []string{"Owned/Earlier"}
			if !duplicate {
				wantNames = append(wantNames, "Dep", "Later")
				wantFrom = append(wantFrom, "Owned-Dep-1.0.0", "Owned-Later-1.0.0")
				wantTo = append(wantTo, "Owned-Dep-2.0.0", "Owned-Later-2.0.0")
				wantFetches = append(wantFetches, "Owned/Dep", "Owned/Later")
			}
			if !reflect.DeepEqual(fetched, wantFetches) {
				t.Errorf("package fetches = %v, want %v", fetched, wantFetches)
			}
			if !reflect.DeepEqual(downloaded, wantNames) || !reflect.DeepEqual(written, wantNames) {
				t.Errorf("downloads/writes = %v/%v, want %v each", downloaded, written, wantNames)
			}
			if !reflect.DeepEqual(replacedFrom, wantFrom) || !reflect.DeepEqual(replacedTo, wantTo) || len(deleted) != 0 {
				t.Errorf("file replacements/deletions exceeded safe work: %v -> %v; deleted=%v", replacedFrom, replacedTo, deleted)
			}
			wantRecords := slices.Clone(originals)
			for i := range wantRecords {
				m := &wantRecords[i]
				if duplicate && m.Name != "Earlier" {
					continue
				}
				delete(wantFiles, filepath.Join("BepInEx", "plugins", m.FullName(), "plugin.dll"))
				delete(wantFiles, filepath.Join("BepInEx", "plugins", m.FullName(), "settings"))
				release := releases[m.Name]
				m.Version, m.FilePath = release.VersionNumber, filepath.Join(plugins, release.FullName)
				m.Description, m.WebsiteURL = release.Description, release.WebsiteURL
				wantFiles[filepath.Join("BepInEx", "plugins", release.FullName, "plugin.dll")] = "updated DLL " + m.Name
			}
			assertModReplacementState(t, server, stored, wantFiles, wantRecords)
			for _, m := range originals {
				if duplicate && m.Name != "Earlier" {
					for _, fileName := range []string{"plugin.dll", "settings"} {
						info, err := os.Stat(filepath.Join(m.FilePath, fileName))
						if err != nil || info.Mode().Perm() != 0o600 {
							t.Errorf("untouched %s/%s missing or changed mode: %v, %v", m.FullName(), fileName, info, err)
						}
					}
				}
			}
			modReplacementFixture{server: server, backups: backups}.assertNoArtifacts(t)
		})
	}
}
