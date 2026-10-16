package service_test

import (
	"bytes"
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

func TestBulkUpdate_InstalledDependencyCoordination(t *testing.T) {
	for name, tt := range map[string]struct {
		rootDependencies   []string
		lastDependencies   []string
		dependencyFirst    bool
		dependencyCurrent  bool
		dependencyConflict bool
		failMiddle         bool
		cleanupWarning     bool
		updated            []string
		downloads          []string
	}{
		"given independent packages, when updating all, then replace each and its direct dependencies": {
			updated:   []string{"Owner/Root", "Owner/Middle", "Owner/Dependency", "Owner/Last", "Owner/Leaf"},
			downloads: []string{"Owner/Root", "Owner/Middle", "Owner/Dependency", "Owner/Leaf", "Owner/Last"},
		},
		"given a root before its installed dependency, when updating all, then replace the dependency once and continue": {
			rootDependencies: []string{"Owner-Dependency-1.0.0", "Other-Dependency-1.0.0"},
			updated:          []string{"Owner/Root", "Owner/Middle", "Owner/Dependency", "Owner/Last", "Owner/Leaf"},
			downloads:        []string{"Owner/Root", "Owner/Dependency", "Owner/Middle", "Owner/Leaf", "Owner/Last"},
		},
		"given the dependency was already updated by its own turn, when a later root needs it, then keep its new files": {
			rootDependencies: []string{"Owner-Dependency-1.0.0"}, dependencyFirst: true,
			updated:   []string{"Owner/Root", "Owner/Middle", "Owner/Dependency", "Owner/Last", "Owner/Leaf"},
			downloads: []string{"Owner/Dependency", "Owner/Leaf", "Owner/Root", "Owner/Middle", "Owner/Last"},
		},
		"given two selected roots share a dependency, when updating all, then publish it once and preserve its own dependency work": {
			rootDependencies: []string{"Owner-Dependency-1.0.0"}, lastDependencies: []string{"Owner-Dependency-1.0.0"},
			updated:   []string{"Owner/Root", "Owner/Middle", "Owner/Dependency", "Owner/Last", "Owner/Leaf"},
			downloads: []string{"Owner/Root", "Owner/Dependency", "Owner/Middle", "Owner/Leaf", "Owner/Last"},
		},
		"given a current installed dependency, when updating a root, then do not replace it or recursively install its dependencies": {
			rootDependencies: []string{"Owner-Dependency-1.0.0"}, dependencyCurrent: true,
			updated:   []string{"Owner/Root", "Owner/Middle", "Owner/Last"},
			downloads: []string{"Owner/Root", "Owner/Middle", "Owner/Last"},
		},
		"given an intervening package failure, when updating all, then the earlier root already has its updated dependency": {
			rootDependencies: []string{"Owner-Dependency-1.0.0"}, failMiddle: true,
			updated:   []string{"Owner/Root", "Owner/Dependency"},
			downloads: []string{"Owner/Root", "Owner/Dependency"},
		},
		"given an unrelated dependency destination, when updating its root, then refuse the conflict without discarding either release": {
			rootDependencies: []string{"Owner-Dependency-1.0.0"}, dependencyConflict: true,
			updated: []string{"Owner/Root"}, downloads: []string{"Owner/Root"},
		},
		"given dependency cleanup warns, when updating all, then continue its selected turn and finish the request": {
			rootDependencies: []string{"Owner-Dependency-1.0.0"}, cleanupWarning: true,
			updated:   []string{"Owner/Root", "Owner/Middle", "Owner/Dependency", "Owner/Last", "Owner/Leaf"},
			downloads: []string{"Owner/Root", "Owner/Dependency", "Owner/Middle", "Owner/Leaf", "Owner/Last"},
		},
		"given dependency cleanup warns, when an intervening package fails, then retain both causes without request completion": {
			rootDependencies: []string{"Owner-Dependency-1.0.0"}, cleanupWarning: true, failMiddle: true,
			updated:   []string{"Owner/Root", "Owner/Dependency"},
			downloads: []string{"Owner/Root", "Owner/Dependency"},
		},
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
			identities := [][2]string{{"Owner", "Root"}, {"Owner", "Middle"}, {"Owner", "Dependency"}, {"Owner", "Last"}, {"Other", "Dependency"}}
			originals := make(map[string]mod.Mod)
			wantFiles := map[string]string{"world.db": "unrelated server bytes"}
			releases := make(map[string]thunderstore.Release)
			var order []string
			for _, identity := range identities {
				key := identity[0] + "/" + identity[1]
				current := mod.Mod{
					Namespace: identity[0], Name: identity[1], Version: "1.0.0", FrameworkID: 1,
					WebsiteURL: "https://example.invalid/original/" + key, Description: "original " + key,
				}
				if key == "Owner/Dependency" && tt.dependencyCurrent {
					current.Version = "2.0.0"
				}
				current.FilePath = filepath.Join(plugins, current.FullName())
				if err := stored.InsertMod(current); err != nil {
					t.Fatal(err)
				}
				persisted, err := stored.GetModByIdentity(current.Namespace, current.Name)
				if err != nil {
					t.Fatal(err)
				}
				originals[key] = persisted
				order = append(order, key)
				wantFiles[filepath.Join("BepInEx", "plugins", current.FullName(), "plugin.dll")] = "original DLL " + key
				wantFiles[filepath.Join("BepInEx", "plugins", current.FullName(), "settings")] = "original settings " + key
				version := "2.0.0"
				if key == "Other/Dependency" {
					version = "1.0.0"
				}
				releases[key] = thunderstore.Release{
					Namespace: current.Namespace, Name: current.Name, VersionNumber: version,
					FullName: current.Namespace + "-" + current.Name + "-" + version, DownloadURL: key,
					WebsiteURL: "https://example.invalid/new/" + key, Description: "new " + key,
				}
			}
			if tt.dependencyConflict {
				wantFiles[filepath.Join("BepInEx", "plugins", "Owner-Dependency-2.0.0", "unrelated")] = "pre-existing destination bytes"
			}
			for path, contents := range wantFiles {
				target := filepath.Join(server, path)
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.dependencyFirst {
				order = []string{"Owner/Dependency", "Owner/Root", "Owner/Middle", "Owner/Last", "Other/Dependency"}
			}
			root := releases["Owner/Root"]
			root.Dependencies = tt.rootDependencies
			releases["Owner/Root"] = root
			last := releases["Owner/Last"]
			last.Dependencies = tt.lastDependencies
			releases["Owner/Last"] = last
			dependency := releases["Owner/Dependency"]
			dependency.Dependencies = []string{"Owner-Leaf-1.0.0"}
			releases["Owner/Dependency"] = dependency
			releases["Owner/Leaf"] = thunderstore.Release{
				Namespace: "Owner", Name: "Leaf", VersionNumber: "2.0.0", FullName: "Owner-Leaf-2.0.0",
				DownloadURL: "Owner/Leaf", Dependencies: []string{"Owner-NotRecursive-1.0.0"},
			}
			var downloads []string
			manager := file.NewManager(&mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				if _, ok := releases[url]; !ok {
					t.Fatalf("unexpected download %q", url)
				}
				downloads = append(downloads, url)
				return &http.Response{Body: io.NopCloser(bytes.NewReader(modIdentityArchive(t, "new DLL "+url)))}, nil
			}}, server)
			cleanupCause, lookupCause := errors.New("dependency cleanup warning"), errors.New("intervening package unavailable")
			files := &mock.Manager{
				ValidateModInstallFunc: manager.ValidateModInstall,
				InstallModFunc:         manager.InstallMod,
				ReplaceModFunc: func(url, oldName, newName string) (file.ModReplacement, error) {
					replacement, err := manager.ReplaceMod(url, oldName, newName)
					if err != nil || !tt.cleanupWarning || url != "Owner/Dependency" {
						return replacement, err
					}
					return &mock.ModReplacement{
						PathFunc: replacement.Path, RecoveryPathFunc: replacement.RecoveryPath,
						CommitFunc: func() error {
							if err := replacement.Commit(); err != nil {
								return err
							}
							return &file.ModInstallCleanupError{Err: cleanupCause}
						},
					}, nil
				},
			}
			records := &mock.ModsRepo{
				ListModsFunc: func() ([]mod.Mod, error) {
					var mods []mod.Mod
					for _, key := range order {
						mods = append(mods, originals[key])
					}
					return mods, nil
				},
				UpsertModFunc: stored.UpsertMod,
			}
			catalog := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
				key := namespace + "/" + name
				if key == "Owner/Middle" && len(tt.rootDependencies) > 0 {
					m, err := stored.GetModByIdentity("Owner", "Dependency")
					if err != nil || m.Version != "2.0.0" {
						t.Fatalf("dependency was deferred past the next package: %+v, %v", m, err)
					}
					if !tt.dependencyCurrent {
						if _, err := os.Stat(originals["Owner/Dependency"].FilePath); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("dependency's obsolete release was not retired before the next package: %v", err)
						}
					}
					if tt.failMiddle {
						return thunderstore.Package{}, lookupCause
					}
				}
				release, ok := releases[key]
				if !ok {
					t.Fatalf("unexpected recursive or mismatched package fetch: %s", key)
				}
				return thunderstore.Package{Latest: release}, nil
			}}
			err := service.NewModService(records, files, catalog, strings.NewReader("Y\n")).UpdateAllMods()
			if _, completed := err.(*service.ModMutationCleanupError); completed != (tt.cleanupWarning && !tt.failMiddle) {
				t.Errorf("incorrect whole-request completion: %v", err)
			}
			if tt.dependencyConflict {
				if !errors.Is(err, service.ErrAddDependenciesFailed) || !errors.Is(err, os.ErrExist) {
					t.Errorf("unrelated destination refusal lost: %v", err)
				}
			} else if tt.failMiddle {
				if !errors.Is(err, lookupCause) || !errors.Is(err, service.ErrModNotFound) {
					t.Errorf("intervening failure lost: %v", err)
				}
			} else if !tt.cleanupWarning && err != nil {
				t.Errorf("valid bulk update failed: %v", err)
			}
			if tt.cleanupWarning && !errors.Is(err, cleanupCause) {
				t.Errorf("dependency cleanup cause lost: %v", err)
			}
			var wantRecords []mod.Mod
			for _, identity := range identities {
				key := identity[0] + "/" + identity[1]
				want := originals[key]
				if slices.Contains(tt.updated, key) {
					delete(wantFiles, filepath.Join("BepInEx", "plugins", want.FullName(), "plugin.dll"))
					delete(wantFiles, filepath.Join("BepInEx", "plugins", want.FullName(), "settings"))
					release := releases[key]
					want.Version, want.Description, want.WebsiteURL = release.VersionNumber, release.Description, release.WebsiteURL
					want.FilePath = filepath.Join(plugins, release.FullName)
					wantFiles[filepath.Join("BepInEx", "plugins", release.FullName, "plugin.dll")] = "new DLL " + key
				}
				wantRecords = append(wantRecords, want)
			}
			if slices.Contains(tt.updated, "Owner/Leaf") {
				wantRecords = append(wantRecords, mod.Mod{
					ID: len(identities) + 1, Namespace: "Owner", Name: "Leaf", Version: "2.0.0",
					FilePath: filepath.Join(plugins, "Owner-Leaf-2.0.0"),
				})
				wantFiles[filepath.Join("BepInEx", "plugins", "Owner-Leaf-2.0.0", "plugin.dll")] = "new DLL Owner/Leaf"
			}
			assertModReplacementState(t, server, stored, wantFiles, wantRecords)
			if !reflect.DeepEqual(downloads, tt.downloads) {
				t.Errorf("downloads = %v, want %v", downloads, tt.downloads)
			}
			modReplacementFixture{server: server, backups: backups}.assertNoArtifacts(t)
		})
	}
}
