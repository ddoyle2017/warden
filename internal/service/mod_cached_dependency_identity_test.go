package service_test

import (
	"bytes"
	"cmp"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

func TestBulkUpdate_CachedPlanChecksIdentityAtEncounter(t *testing.T) {
	for name, duplicate := range map[string]bool{
		"given a cached selected dependency plan, when its dependency is unique, then finish without reinstalling selected packages":             false,
		"given a cached selected dependency plan, when its dependency is duplicated, then retain earlier replacements and stop before its fetch": true,
	} {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			db := th.CreateDatabase()
			repo.CreateFrameworksTable(db)
			repo.CreateModsTable(db)
			th.SeedFrameworksTable(repo.NewFrameworksRepo(db))
			stored := repo.NewModsRepo(db)
			server := th.GetValheimDirectory()
			plugins := filepath.Join(server, "BepInEx", "plugins")
			originals := []mod.Mod{
				{Namespace: "Owned", Name: "Earlier", Version: "1.0.0", FrameworkID: 1},
				{Namespace: "Owned", Name: "B", Version: "1.0.0", FrameworkID: 1},
				{Namespace: "Owned", Name: "D", Version: "1.0.0", FrameworkID: 1},
			}
			if duplicate {
				originals = append(originals, mod.Mod{Namespace: "Owned", Name: "D", Version: "1.1.0", FrameworkID: 1})
			}
			originals = append(originals, mod.Mod{Namespace: "Owned", Name: "Later", Version: "1.0.0", FrameworkID: 1})
			wantFiles := map[string]string{"server-data": "unrelated server bytes"}
			for _, m := range originals {
				m.FilePath = filepath.Join(plugins, m.FullName())
				m.Description = "original " + m.FullName()
				m.WebsiteURL = "https://example.invalid/original/" + m.FullName()
				if err := stored.InsertMod(m); err != nil {
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
			releases := map[string]thunderstore.Release{}
			for _, name := range []string{"Earlier", "B", "D", "Later"} {
				releases[name] = thunderstore.Release{
					Namespace: "Owned", Name: name, VersionNumber: "2.0.0", FullName: "Owned-" + name + "-2.0.0",
					DownloadURL: name, Description: "updated " + name, WebsiteURL: "https://example.invalid/new/" + name,
				}
			}
			earlier := releases["Earlier"]
			earlier.Dependencies = []string{"Owned-B-1.0.0"}
			releases["Earlier"] = earlier
			b := releases["B"]
			b.Dependencies = []string{"Owned-D-1.0.0"}
			releases["B"] = b

			var fetched, downloaded, replacedFrom, replacedTo, written []string
			manager := file.NewManager(&mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				if _, ok := releases[url]; !ok {
					t.Fatalf("unexpected download %q", url)
				}
				downloaded = append(downloaded, url)
				return &http.Response{Body: io.NopCloser(bytes.NewReader(modIdentityArchive(t, "updated DLL "+url)))}, nil
			}}, server)
			files := &mock.Manager{
				ValidateModInstallFunc: manager.ValidateModInstall,
				ReplaceModFunc: func(url, oldName, newName string) (file.ModReplacement, error) {
					replacedFrom = append(replacedFrom, oldName)
					replacedTo = append(replacedTo, newName)
					return manager.ReplaceMod(url, oldName, newName)
				},
				InstallModFunc: func(string, string) (string, error) {
					t.Fatal("a selected package used ordinary installation")
					return "", nil
				},
			}
			records := &mock.ModsRepo{
				ListModsFunc: func() ([]mod.Mod, error) { return slices.Clone(originals), nil },
				UpsertModFunc: func(m mod.Mod) error {
					written = append(written, m.Name)
					return stored.UpsertMod(m)
				},
			}
			catalog := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
				fetched = append(fetched, namespace+"/"+name)
				release, ok := releases[name]
				if namespace != "Owned" || !ok {
					t.Fatalf("unexpected package %s/%s", namespace, name)
				}
				return thunderstore.Package{Latest: release}, nil
			}}

			err = service.NewModService(records, files, catalog, strings.NewReader("Y\n")).UpdateAllMods()
			if duplicate {
				for _, cause := range []error{service.ErrAddDependenciesFailed, service.ErrUnableToUpdateMod, service.ErrDuplicateModRecords, repo.ErrModFetchMultipleResults} {
					if !errors.Is(err, cause) {
						t.Errorf("duplicate identity rejection lost %v: %v", cause, err)
					}
				}
				if err == nil || !strings.Contains(err.Error(), "Owned/D") {
					t.Errorf("duplicate rejection lost the identity: %v", err)
				}
				if errors.Is(err, service.ErrInvalidDependency) || errors.Is(err, service.ErrInvalidReleaseVersion) {
					t.Errorf("valid cached syntax was misclassified: %v", err)
				}
				if _, completed := err.(*service.ModMutationCleanupError); completed {
					t.Errorf("duplicate rejection reported request completion: %v", err)
				}
			} else if err != nil {
				t.Fatalf("unique cached-plan control failed: %v", err)
			}
			wantNames := []string{"Earlier", "B"}
			wantFetches := []string{"Owned/Earlier", "Owned/B"}
			wantFrom := []string{"Owned-Earlier-1.0.0", "Owned-B-1.0.0"}
			wantTo := []string{"Owned-Earlier-2.0.0", "Owned-B-2.0.0"}
			if !duplicate {
				wantNames = append(wantNames, "D", "Later")
				wantFetches = append(wantFetches, "Owned/D", "Owned/Later")
				wantFrom = append(wantFrom, "Owned-D-1.0.0", "Owned-Later-1.0.0")
				wantTo = append(wantTo, "Owned-D-2.0.0", "Owned-Later-2.0.0")
			}
			if !slices.Equal(fetched, wantFetches) || !slices.Equal(downloaded, wantNames) || !slices.Equal(written, wantNames) ||
				!slices.Equal(replacedFrom, wantFrom) || !slices.Equal(replacedTo, wantTo) {
				t.Errorf("unexpected cached-plan effects: fetched=%v downloaded=%v written=%v replaced=%v -> %v; want %v/%v/%v -> %v",
					fetched, downloaded, written, replacedFrom, replacedTo, wantFetches, wantNames, wantFrom, wantTo)
			}
			wantRecords := slices.Clone(originals)
			for i := range wantRecords {
				m := &wantRecords[i]
				if !slices.Contains(wantNames, m.Name) {
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
			modReplacementFixture{server: server, backups: backups}.assertNoArtifacts(t)
		})
	}
}
