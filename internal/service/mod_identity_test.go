package service_test

import (
	"archive/zip"
	"bytes"
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

func TestRemoveMod_NamespacedIdentity(t *testing.T) {
	for name, tt := range map[string]struct {
		namespace string
		second    string
		fetchErr  error
		wantErr   error
	}{
		"given another author's same-name mod, when removing, then retain its bytes and full metadata": {
			namespace: "AuthorB", wantErr: service.ErrModNotInstalled,
		},
		"given a matching namespace, when removing, then delete only that package": {
			namespace: "AuthorA",
		},
		"given two authors' same-name mods, when removing one, then retain the other": {
			namespace: "AuthorB", second: "AuthorB",
		},
		"given two authors' same-name mods, when removing the first, then retain the second": {
			namespace: "AuthorA", second: "AuthorB",
		},
		"given duplicate records for one identity, when removing, then retain all bytes and records": {
			namespace: "AuthorA", second: "AuthorA", wantErr: service.ErrUnableToRemoveMod,
		},
		"given a lookup failure, when removing, then retain all bytes and records": {
			namespace: "AuthorA", fetchErr: repo.ErrModFetchFailed, wantErr: service.ErrUnableToRemoveMod,
		},
	} {
		t.Run(name, func(t *testing.T) {
			namespaces := []string{"AuthorA"}
			if tt.second != "" {
				namespaces = append(namespaces, tt.second)
			}
			th, mr, mods, backups := newModIdentityFixture(t, namespaces...)
			lookup := mr
			if tt.fetchErr != nil {
				lookup = &mock.ModsRepo{GetModByIdentityFunc: func(namespace, name string) (mod.Mod, error) {
					if namespace != tt.namespace || name != "Shared" {
						t.Fatalf("unexpected lookup %q/%q", namespace, name)
					}
					return mod.Mod{}, tt.fetchErr
				}}
			}
			files := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())
			stages := 0
			manager := &mock.Manager{StageModRemovalFunc: func(fullName string) (file.ModRemoval, error) {
				stages++
				return files.StageModRemoval(fullName)
			}}
			ms := service.NewModService(lookup, manager, &mock.Thunderstore{}, strings.NewReader("Y\n"))

			err := ms.RemoveMod(tt.namespace, "Shared")
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
			if tt.fetchErr != nil && !errors.Is(err, tt.fetchErr) {
				t.Errorf("lookup cause was lost: %v", err)
			}
			if tt.wantErr != nil && stages != 0 {
				t.Errorf("failed selection staged files %d times", stages)
			}
			var want []mod.Mod
			for _, m := range mods {
				removed := tt.wantErr == nil && m.Namespace == tt.namespace
				if removed {
					if _, err := os.Stat(m.FilePath); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("expected %s files removed, got %v", m.FullName(), err)
					}
				} else {
					want = append(want, m)
				}
			}
			assertModIdentityState(t, mr, want)
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("unexpected backup artifacts: %v, %v", entries, err)
			}
		})
	}
}

func TestUpdateMod_NamespacedIdentity(t *testing.T) {
	for name, tt := range map[string]struct {
		namespace string
		second    string
		nameOnly  bool
		all       bool
		apiFail   bool
		lookupErr error
		updated   []string
		wantErr   error
	}{
		"given two authors, when updating the first, then preserve the second's bytes and metadata": {
			namespace: "AuthorA", second: "AuthorB", updated: []string{"AuthorA"},
		},
		"given two authors, when updating the second, then preserve the first's bytes and metadata": {
			namespace: "AuthorB", second: "AuthorB", updated: []string{"AuthorB"},
		},
		"given one matching name, when updating without namespace, then preserve the existing syntax": {
			nameOnly: true, updated: []string{"AuthorA"},
		},
		"given two matching names, when updating without namespace, then stop before fetch or files": {
			nameOnly: true, second: "AuthorB", wantErr: service.ErrModNameAmbiguous,
		},
		"given another author's mod, when updating an explicit namespace, then do not fall back": {
			namespace: "Missing", wantErr: service.ErrModNotInstalled,
		},
		"given an empty explicit namespace, when updating, then do not fall back to a name match": {
			wantErr: service.ErrModNotInstalled,
		},
		"given duplicate records for one identity, when updating, then stop before fetch or files": {
			namespace: "AuthorA", second: "AuthorA", wantErr: service.ErrUnableToUpdateMod,
		},
		"given a failed package fetch, when updating, then preserve both installations": {
			namespace: "AuthorA", second: "AuthorB", apiFail: true, wantErr: service.ErrModNotFound,
		},
		"given a failed exact lookup, when updating, then stop before fetch or files": {
			namespace: "AuthorA", lookupErr: repo.ErrModFetchFailed, wantErr: service.ErrUnableToUpdateMod,
		},
		"given a failed name-only lookup, when updating, then stop before fetch or files": {
			nameOnly: true, lookupErr: repo.ErrModFetchFailed, wantErr: service.ErrUnableToUpdateMod,
		},
		"given two authors, when updating all, then update each package independently": {
			second: "AuthorB", all: true, updated: []string{"AuthorA", "AuthorB"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			namespaces := []string{"AuthorA"}
			if tt.second != "" {
				namespaces = append(namespaces, tt.second)
			}
			th, mr, original, backups := newModIdentityFixture(t, namespaces...)
			downloads, fetches := 0, 0
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				for _, namespace := range tt.updated {
					if url == "https://example.invalid/"+namespace {
						downloads++
						return &http.Response{Body: io.NopCloser(bytes.NewReader(modIdentityArchive(t, namespace+" 2.0.0 bytes")))}, nil
					}
				}
				t.Fatalf("unexpected download %q", url)
				return nil, nil
			}}
			ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
				fetches++
				if name != "Shared" || (tt.apiFail && namespace != tt.namespace) || (!tt.apiFail && !slices.Contains(tt.updated, namespace)) {
					t.Fatalf("unexpected package fetch %q/%q", namespace, name)
				}
				if tt.apiFail {
					return thunderstore.Package{}, thunderstore.ErrThunderstoreAPI
				}
				return thunderstore.Package{Latest: thunderstore.Release{
					Namespace: namespace, Name: "Shared", VersionNumber: "2.0.0",
					FullName: namespace + "-Shared-2.0.0", DownloadURL: "https://example.invalid/" + namespace,
					WebsiteURL: "https://example.invalid/latest/" + namespace, Description: namespace + " updated metadata",
				}}, nil
			}}
			lookup := mr
			if tt.lookupErr != nil {
				faulted := &mock.ModsRepo{}
				if tt.nameOnly {
					faulted.GetModFunc = func(name string) (mod.Mod, error) {
						if name != "Shared" {
							t.Fatalf("unexpected lookup %q", name)
						}
						return mod.Mod{}, tt.lookupErr
					}
				} else {
					faulted.GetModByIdentityFunc = func(namespace, name string) (mod.Mod, error) {
						if namespace != tt.namespace || name != "Shared" {
							t.Fatalf("unexpected lookup %q/%q", namespace, name)
						}
						return mod.Mod{}, tt.lookupErr
					}
				}
				lookup = faulted
			}
			files := file.NewManager(client, th.GetValheimDirectory())
			mutations := 0
			manager := &mock.Manager{
				ValidateModInstallFunc: files.ValidateModInstall,
				RemoveModFunc: func(fullName string) error {
					mutations++
					return files.RemoveMod(fullName)
				},
				InstallModFunc: func(url, fullName string) (string, error) {
					mutations++
					return files.InstallMod(url, fullName)
				},
			}
			ms := service.NewModService(lookup, manager, ts, strings.NewReader("Y\n"))
			var err error
			if tt.all {
				err = ms.UpdateAllMods()
			} else if tt.nameOnly {
				err = ms.UpdateMod("Shared")
			} else {
				err = ms.UpdateModByIdentity(tt.namespace, "Shared")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
			if tt.wantErr == service.ErrModNameAmbiguous && (!errors.Is(err, service.ErrUnableToUpdateMod) || !errors.Is(err, repo.ErrModFetchMultipleResults)) {
				t.Errorf("ambiguous update lost its error classifications: %v", err)
			}
			if tt.lookupErr != nil && !errors.Is(err, tt.lookupErr) {
				t.Errorf("lookup cause was lost: %v", err)
			}
			if tt.wantErr != nil && mutations != 0 {
				t.Errorf("failed selection or fetch reached %d file mutations", mutations)
			}
			want := slices.Clone(original)
			for i := range want {
				if slices.Contains(tt.updated, want[i].Namespace) {
					if _, err := os.Stat(want[i].FilePath); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("old release remains: %v", err)
					}
					want[i].Version = "2.0.0"
					want[i].FilePath = filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory, want[i].FullName())
					want[i].WebsiteURL = "https://example.invalid/latest/" + want[i].Namespace
					want[i].Description = want[i].Namespace + " updated metadata"
				}
			}
			assertModIdentityState(t, mr, want)
			wantFetches := len(tt.updated)
			if tt.apiFail {
				wantFetches = 1
			}
			if fetches != wantFetches || downloads != len(tt.updated) {
				t.Errorf("fetches/downloads = %d/%d, want %d/%d", fetches, downloads, wantFetches, len(tt.updated))
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("unexpected backup artifacts: %v, %v", entries, err)
			}
		})
	}
}

func TestAddMod_NamespacedIdentity(t *testing.T) {
	for name, dependency := range map[string]bool{
		"given another author's same-name mod, when adding, then install a separate package":              false,
		"given another author's same-name mod, when installing a dependency, then retain both identities": true,
	} {
		t.Run(name, func(t *testing.T) {
			th, mr, original, _ := newModIdentityFixture(t, "AuthorA")
			target := mod.Mod{
				ID: 2, Namespace: "AuthorB", Name: "Shared", Version: "2.0.0",
				WebsiteURL: "https://example.invalid/b", Description: "new author metadata",
			}
			want := slices.Clone(original)
			namespace, modName := target.Namespace, target.Name
			packages := map[string]thunderstore.Release{}
			if dependency {
				namespace, modName = "Owner", "Root"
				root := mod.Mod{ID: 2, Namespace: namespace, Name: modName, Version: "1.0.0"}
				root.FilePath = filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory, root.FullName())
				want = append(want, root)
				target.ID = 3
				packages["Owner/Root"] = thunderstore.Release{
					Namespace: namespace, Name: modName, VersionNumber: root.Version,
					FullName: root.FullName(), DownloadURL: "https://example.invalid/root",
					Dependencies: []string{"AuthorB-Shared-1.0.0"},
				}
			}
			target.FilePath = filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory, target.FullName())
			want = append(want, target)
			packages["AuthorB/Shared"] = thunderstore.Release{
				Namespace: target.Namespace, Name: target.Name, VersionNumber: target.Version,
				FullName: target.FullName(), DownloadURL: "https://example.invalid/shared",
				WebsiteURL: target.WebsiteURL, Description: target.Description,
			}
			fetches, downloads := 0, 0
			ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
				fetches++
				release, ok := packages[namespace+"/"+name]
				if !ok {
					t.Fatalf("unexpected package %q/%q", namespace, name)
				}
				return thunderstore.Package{Latest: release}, nil
			}}
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				downloads++
				contents := "AuthorB 2.0.0 bytes"
				if dependency && url == "https://example.invalid/root" {
					contents = "Owner 1.0.0 bytes"
				} else if url != "https://example.invalid/shared" {
					t.Fatalf("unexpected download %q", url)
				}
				return &http.Response{Body: io.NopCloser(bytes.NewReader(modIdentityArchive(t, contents)))}, nil
			}}
			ms := service.NewModService(mr, file.NewManager(client, th.GetValheimDirectory()), ts, strings.NewReader(""))
			if err := ms.AddMod(namespace, modName); err != nil {
				t.Fatal(err)
			}
			assertModIdentityState(t, mr, want)
			if fetches != len(packages) || downloads != len(packages) {
				t.Errorf("fetches/downloads = %d/%d, want %d each", fetches, downloads, len(packages))
			}
		})
	}
}

func newModIdentityFixture(t *testing.T, namespaces ...string) (helper.Helper, repo.Mods, []mod.Mod, string) {
	t.Helper()
	th := helper.NewHelper(t)
	backups := t.TempDir()
	t.Setenv("TMPDIR", backups)
	db := th.CreateDatabase()
	repo.CreateFrameworksTable(db)
	repo.CreateModsTable(db)
	th.SeedFrameworksTable(repo.NewFrameworksRepo(db))
	mr := repo.NewModsRepo(db)
	var mods []mod.Mod
	for i, namespace := range namespaces {
		m := mod.Mod{
			ID: i + 1, FrameworkID: 1, Namespace: namespace, Name: "Shared", Version: "1.0.0",
			WebsiteURL: "https://example.invalid/" + namespace, Description: namespace + " original metadata",
		}
		m.FilePath = filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory, m.FullName())
		if err := os.MkdirAll(m.FilePath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(m.FilePath, "plugin.dll"), []byte(namespace+" 1.0.0 bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := mr.InsertMod(m); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, m)
	}
	return th, mr, mods, backups
}

func assertModIdentityState(t *testing.T, mr repo.Mods, want []mod.Mod) {
	t.Helper()
	got, err := mr.ListMods()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Errorf("expected %d records, got %+v", len(want), got)
	}
	for _, m := range want {
		if !slices.ContainsFunc(got, func(record mod.Mod) bool { return record.Equals(&m) }) {
			t.Errorf("expected full metadata %+v in %+v", m, got)
		}
		data, err := os.ReadFile(filepath.Join(m.FilePath, "plugin.dll"))
		if err != nil || string(data) != m.Namespace+" "+m.Version+" bytes" {
			t.Errorf("unexpected %s bytes: %q, %v", m.FullName(), data, err)
		}
	}
}

func modIdentityArchive(t *testing.T, contents string) []byte {
	t.Helper()
	var payload bytes.Buffer
	archive := zip.NewWriter(&payload)
	entry, err := archive.Create("plugin.dll")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(entry, contents); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return payload.Bytes()
}
