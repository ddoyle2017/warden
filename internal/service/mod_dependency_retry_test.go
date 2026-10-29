package service_test

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"github.com/ddoyle2017/warden/internal/data/file"
	"github.com/ddoyle2017/warden/internal/data/repo"
	"github.com/ddoyle2017/warden/internal/domain/mod"
	"github.com/ddoyle2017/warden/internal/service"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mattn/go-sqlite3"
)

func TestAddMod_RetryUsesInstalledRelease(t *testing.T) {
	t.Run("given changed latest root metadata, when retrying add, then use the installed version's direct list", func(t *testing.T) {
		f := newModReplacementFixture(t)
		var requests []string
		client := &mock.HTTPClient{
			DoFunc: func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodGet {
					t.Fatalf("unexpected metadata method %q", request.Method)
				}
				endpoint := request.URL.String()
				requests = append(requests, endpoint)
				body := ""
				switch endpoint {
				case "https://thunderstore.io/api/experimental/package/Owned/Installed/1.0.0/":
					body = `{"namespace":"Owned","name":"Installed","version_number":"1.0.0","full_name":"Owned-Installed-1.0.0","dependencies":["Required-Dependency-1.0.0"]}`
				case "https://thunderstore.io/api/experimental/package/Owned/Installed":
					body = `{"latest":{"namespace":"Owned","name":"Installed","version_number":"2.0.0","full_name":"Owned-Installed-2.0.0","dependencies":["Wrong-NewDependency-2.0.0"]}}`
				case "https://thunderstore.io/api/experimental/package/Required/Dependency":
					body = `{"latest":{"namespace":"Required","name":"Dependency","version_number":"2.4.0","full_name":"Required-Dependency-2.4.0","download_url":"dependency-download","dependencies":["Nested-NotTraversed-1.0.0"]}}`
				default:
					t.Fatalf("unexpected metadata request %q", endpoint)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			},
			GetFunc: func(endpoint string) (*http.Response, error) {
				requests = append(requests, endpoint)
				if endpoint != "dependency-download" {
					t.Fatalf("unexpected download request %q", endpoint)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(modIdentityArchive(t, "dependency bytes")))}, nil
			},
		}
		ms := service.NewModService(f.records, file.NewManager(client, f.server), thunderstore.New(client), strings.NewReader(""))
		if err := ms.AddMod("Owned", "Installed"); err != nil {
			t.Fatal(err)
		}
		wantFiles := f.originalFiles()
		wantFiles["BepInEx/plugins/Required-Dependency-2.4.0/plugin.dll"] = "dependency bytes"
		assertModReplacementState(t, f.server, f.records, wantFiles, []mod.Mod{f.original, f.other, {
			ID: 3, Namespace: "Required", Name: "Dependency", Version: "2.4.0",
			FilePath: filepath.Join(f.server, "BepInEx/plugins/Required-Dependency-2.4.0"),
		}})
		if !slices.Equal(requests, []string{
			"https://thunderstore.io/api/experimental/package/Owned/Installed/1.0.0/",
			"https://thunderstore.io/api/experimental/package/Required/Dependency",
			"dependency-download",
		}) {
			t.Errorf("retry used the wrong metadata or downloaded the root: %v", requests)
		}
		f.assertNoArtifacts(t)
	})
}

func TestAddMod_RetryReusesRecordedIdentities(t *testing.T) {
	for name, tt := range map[string]struct {
		tokens []string
		repair bool
	}{
		"no dependencies": {tokens: []string{}},
		"only the framework": {
			tokens: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
		},
		"only recorded dependencies": {
			tokens: []string{"Other-Unrelated-1.0.0", "AuthorA-Shared-9.0.0"},
		},
		"a self-reference": {
			tokens: []string{"Owned-Installed-9.0.0"},
		},
		"recorded and missing same-name packages with duplicate requirements": {
			tokens: []string{
				"Other-Unrelated-1.0.0", "AuthorA-Shared-1.0.0", "AuthorB-Shared-1.0.0",
				"AuthorB-Shared-2.0.0", "Owned-Installed-9.0.0", "denikson-BepInExPack_Valheim-5.4.2202",
			},
			repair: true,
		},
	} {
		t.Run("given "+name+", when retrying add, then install only missing exact identities once", func(t *testing.T) {
			f := newModReplacementFixture(t)
			healthy := mod.Mod{
				ID: 3, Namespace: "AuthorA", Name: "Shared", Version: "4.0.0", FrameworkID: 1,
				FilePath:   filepath.Join(f.server, "BepInEx/plugins/AuthorA-Shared-4.0.0"),
				WebsiteURL: "https://example.invalid/healthy", Description: "keep this complete record",
			}
			if err := f.records.InsertMod(healthy); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(healthy.FilePath, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(healthy.FilePath, "plugin.dll"), []byte("author A bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			fetches, downloads, writes := 0, 0, 0
			ts := &mock.Thunderstore{
				GetReleaseFunc: func(namespace, name, version string) (thunderstore.Release, error) {
					if namespace != "Owned" || name != "Installed" || version != "1.0.0" {
						t.Fatalf("wrong root release %s/%s/%s", namespace, name, version)
					}
					return thunderstore.Release{
						Namespace: namespace, Name: name, VersionNumber: version, FullName: "Owned-Installed-1.0.0",
						Dependencies: slices.Clone(tt.tokens),
					}, nil
				},
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					fetches++
					if namespace != "AuthorB" || name != "Shared" {
						t.Fatalf("recorded dependency was fetched or another namespace selected: %s/%s", namespace, name)
					}
					return thunderstore.Package{Latest: thunderstore.Release{
						Namespace: "AuthorB", Name: "Shared", VersionNumber: "2.0.0",
						FullName: "AuthorB-Shared-2.0.0", DownloadURL: "shared-download",
					}}, nil
				},
			}
			r := &mock.ModsRepo{
				GetModByIdentityFunc: func(namespace, name string) (mod.Mod, error) {
					current, err := f.records.GetModByIdentity(namespace, name)
					if err != nil {
						return current, fmt.Errorf("exact lookup: %w", err)
					}
					return current, nil
				},
				UpsertModFunc: func(m mod.Mod) error {
					writes++
					if m.Namespace != "AuthorB" || m.Name != "Shared" {
						t.Fatalf("retry overwrote a recorded package: %+v", m)
					}
					return f.records.UpsertMod(m)
				},
			}
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				downloads++
				if url != "shared-download" {
					t.Fatalf("unexpected download %q", url)
				}
				return &http.Response{Body: io.NopCloser(bytes.NewReader(modIdentityArchive(t, "author B bytes")))}, nil
			}}
			err := service.NewModService(r, file.NewManager(client, f.server), ts, strings.NewReader("")).AddMod("Owned", "Installed")
			wantErr, wantWork := service.ErrModAlreadyInstalled, 0
			wantFiles, wantRecords := f.originalFiles(), []mod.Mod{f.original, f.other, healthy}
			wantFiles["BepInEx/plugins/AuthorA-Shared-4.0.0/plugin.dll"] = "author A bytes"
			if tt.repair {
				wantErr, wantWork = nil, 1
				wantFiles["BepInEx/plugins/AuthorB-Shared-2.0.0/plugin.dll"] = "author B bytes"
				wantRecords = append(wantRecords, mod.Mod{
					ID: 4, Namespace: "AuthorB", Name: "Shared", Version: "2.0.0",
					FilePath: filepath.Join(f.server, "BepInEx/plugins/AuthorB-Shared-2.0.0"),
				})
			}
			if !errors.Is(err, wantErr) || fetches != wantWork || downloads != wantWork || writes != wantWork {
				t.Errorf("result=%v fetches/downloads/writes=%d/%d/%d, want %v and %d each", err, fetches, downloads, writes, wantErr, wantWork)
			}
			assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
			f.assertNoArtifacts(t)
		})
	}
}

func TestAddMod_RetryRejectsUnknownMetadata(t *testing.T) {
	const metadata = `{"namespace":"Owned","name":"Installed","version_number":"1.0.0","full_name":"Owned-Installed-1.0.0","dependencies":[]}`
	for name, tt := range map[string]struct {
		body                          string
		status                        int
		wantErr                       error
		invalidVersion, invalidStored bool
	}{
		"an unavailable installed version": {
			body: `{}`, status: http.StatusNotFound, wantErr: thunderstore.ErrPackageNotFound,
		},
		"the wrong root version": {
			body: strings.ReplaceAll(metadata, "1.0.0", "2.0.0"), wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"the wrong version field with a matching root full name": {
			body: strings.Replace(metadata, `"version_number":"1.0.0"`, `"version_number":"2.0.0"`, 1), wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"an omitted dependency list": {
			body: strings.Replace(metadata, `,"dependencies":[]`, "", 1), wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"a null dependency list": {
			body: strings.Replace(metadata, `[]`, `null`, 1), wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"a malformed first dependency": {
			body: strings.Replace(metadata, `[]`, `["malformed","Required-First-1.0.0"]`, 1), wantErr: service.ErrInvalidDependency,
		},
		"a malformed later dependency": {
			body: strings.Replace(metadata, `[]`, `["Required-First-1.0.0","malformed"]`, 1), wantErr: service.ErrInvalidDependency,
		},
		"a malformed framework token": {
			body:    strings.Replace(metadata, `[]`, `["Required-First-1.0.0","denikson-BepInExPack_Valheim-invalid"]`, 1),
			wantErr: service.ErrInvalidDependency, invalidVersion: true,
		},
		"an invalid dependency version": {
			body:    strings.Replace(metadata, `[]`, `["Required-First-1.0.0","Required-Later-1.0.0-01"]`, 1),
			wantErr: service.ErrInvalidDependency, invalidVersion: true,
		},
		"an invalid stored root version": {
			wantErr: service.ErrInvalidReleaseVersion, invalidVersion: true, invalidStored: true,
		},
	} {
		t.Run("given "+name+", when retrying add, then preserve working state without guessing", func(t *testing.T) {
			f := newModReplacementFixture(t)
			if tt.invalidStored {
				f.original.Version = "invalid"
				if err := f.records.UpdateMod(f.original); err != nil {
					t.Fatal(err)
				}
			}
			requests, lookups := 0, 0
			client := &mock.HTTPClient{DoFunc: func(request *http.Request) (*http.Response, error) {
				requests++
				if request.Method != http.MethodGet || request.URL.String() != "https://thunderstore.io/api/experimental/package/Owned/Installed/1.0.0/" {
					t.Fatalf("unexpected request after unsafe metadata: %s %s", request.Method, request.URL)
				}
				status := tt.status
				if status == 0 {
					status = http.StatusOK
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			}}
			r := &mock.ModsRepo{GetModByIdentityFunc: func(namespace, name string) (mod.Mod, error) {
				lookups++
				if namespace != "Owned" || name != "Installed" || lookups != 1 {
					t.Fatalf("invalid metadata reached dependency lookup: %s/%s", namespace, name)
				}
				return f.records.GetModByIdentity(namespace, name)
			}}
			err := service.NewModService(r, file.NewManager(client, f.server), thunderstore.New(client), strings.NewReader("")).AddMod("Owned", "Installed")
			if !errors.Is(err, service.ErrAddDependenciesFailed) || !errors.Is(err, tt.wantErr) ||
				errors.Is(err, service.ErrInvalidReleaseVersion) != tt.invalidVersion {
				t.Errorf("error = %v, want dependency failure preserving %v and invalid version=%v", err, tt.wantErr, tt.invalidVersion)
			}
			if errors.Is(err, service.ErrModAlreadyInstalled) {
				t.Errorf("unknown completeness reported as already installed: %v", err)
			}
			if !tt.invalidVersion && !errors.Is(err, service.ErrInvalidDependency) &&
				(err == nil || !strings.Contains(err.Error(), "installed Owned-Installed-1.0.0") || !strings.Contains(err.Error(), "root left unchanged")) {
				t.Errorf("metadata failure lost installed-version recovery context: %v", err)
			}
			wantRequests := 1
			if tt.invalidStored {
				wantRequests = 0
			}
			if requests != wantRequests || lookups != 1 {
				t.Errorf("requests/lookups = %d/%d, want %d/1", requests, lookups, wantRequests)
			}
			assertModReplacementState(t, f.server, f.records, f.originalFiles(), []mod.Mod{f.original, f.other})
			f.assertNoArtifacts(t)
		})
	}
}

func TestAddMod_RetryLookupFailuresAreNotAbsence(t *testing.T) {
	for _, target := range []string{"root", "dependency"} {
		for _, failure := range []string{"query", "mapping", "iteration before", "iteration after", "ambiguity"} {
			t.Run("given a "+target+" "+failure+" failure, when retrying add, then stop without changing files or records", func(t *testing.T) {
				f := newModReplacementFixture(t)
				broken := f.original
				if target == "dependency" {
					broken = f.other
				}
				wantRecords := []mod.Mod{f.original, f.other}
				if failure == "ambiguity" {
					if err := f.records.InsertMod(broken); err != nil {
						t.Fatal(err)
					}
					broken.ID = 3
					wantRecords = append(wantRecords, broken)
				}
				var driverErr error
				var rows *sql.Rows
				lookup := repo.NewModsRepo(&mock.Database{QueryFunc: func(query string, args ...any) (*sql.Rows, error) {
					if len(args) != 2 {
						t.Fatalf("retry did not use an exact identity: %v", args)
					}
					if args[0] == broken.Namespace && args[1] == broken.Name {
						switch failure {
						case "query":
							query = strings.Replace(query, "FROM mods", "FROM missing_mods", 1)
						case "mapping":
							query = strings.Replace(query, "SELECT id,", "SELECT NULL,", 1)
						case "iteration before":
							query = strings.Replace(query, "SELECT id,", "SELECT abs(-9223372036854775808),", 1)
						case "iteration after":
							query += ` UNION ALL SELECT abs(-9223372036854775808), '', '', '', '', '', '', 0`
						}
						rows, driverErr = f.db.Query(query, args...)
						return rows, driverErr
					}
					return f.db.Query(query, args...)
				}})
				requests := 0
				ts := &mock.Thunderstore{GetReleaseFunc: func(namespace, name, version string) (thunderstore.Release, error) {
					requests++
					if target != "dependency" || namespace != "Owned" || name != "Installed" || version != "1.0.0" {
						t.Fatalf("unexpected release lookup %s/%s/%s", namespace, name, version)
					}
					return thunderstore.Release{
						Namespace: namespace, Name: name, VersionNumber: version, FullName: "Owned-Installed-1.0.0",
						Dependencies: []string{"Missing-First-1.0.0", "Other-Unrelated-1.0.0"},
					}, nil
				}}
				err := service.NewModService(lookup, &mock.Manager{}, ts, strings.NewReader("")).AddMod("Owned", "Installed")
				wantPublic, wantRepo := service.ErrModInstallFailed, repo.ErrModMappingFailed
				wantRequests := 0
				if target == "dependency" {
					wantPublic, wantRequests = service.ErrAddDependenciesFailed, 1
				}
				switch failure {
				case "query":
					wantRepo = repo.ErrModFetchFailed
				case "ambiguity":
					wantRepo = repo.ErrModFetchMultipleResults
				}
				if !errors.Is(err, wantPublic) || !errors.Is(err, wantRepo) || errors.Is(err, repo.ErrModFetchNoResults) {
					t.Errorf("lookup failure was misclassified: %v, want %v and %v", err, wantPublic, wantRepo)
				}
				if failure == "query" || strings.HasPrefix(failure, "iteration") {
					if rows != nil {
						driverErr = rows.Err()
					}
					var cause sqlite3.Error
					if driverErr == nil || !errors.Is(err, driverErr) || !errors.As(err, &cause) {
						t.Errorf("underlying SQLite failure was lost: %v (driver %v)", err, driverErr)
					}
				}
				if requests != wantRequests {
					t.Errorf("metadata requests = %d, want %d", requests, wantRequests)
				}
				assertModReplacementState(t, f.server, f.records, f.originalFiles(), wantRecords)
				f.assertNoArtifacts(t)
			})
		}
	}
}
func TestAddMod_RetriesMissingDependencies(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, failure := range []string{"lookup", "download", "archive"} {
			state := "a fresh add"
			if existing {
				state = "a pre-existing partial installation"
			}
			t.Run("given "+state+", when dependency "+failure+" fails repeatedly, then repair without reinstalling the root", func(t *testing.T) {
				f := newModReplacementFixture(t)
				root := thunderstore.Release{
					Namespace: "Owned", Name: "Root", VersionNumber: "1.0.0", FullName: "Owned-Root-1.0.0",
					DownloadURL: "root-download", WebsiteURL: "https://example.invalid/root", Description: "root metadata",
					Dependencies: []string{"Required-Dependency-1.0.0"},
				}
				if existing {
					root.Name, root.FullName = f.original.Name, f.original.FullName()
					if f.original.Dependencies != nil {
						t.Fatal("persisted root unexpectedly contains its manifest")
					}
				}
				cause := errors.New("dependency unavailable")
				fail := true
				rootFetches, releaseFetches, dependencyFetches := 0, 0, 0
				rootDownloads, dependencyDownloads := 0, 0
				ts := &mock.Thunderstore{
					GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
						switch {
						case namespace == root.Namespace && name == root.Name:
							rootFetches++
							if existing || rootFetches != 1 {
								t.Fatal("retry fetched the root's latest release")
							}
							return thunderstore.Package{Latest: root}, nil
						case namespace == "Required" && name == "Dependency":
							dependencyFetches++
							if fail && failure == "lookup" {
								return thunderstore.Package{}, cause
							}
							return thunderstore.Package{Latest: thunderstore.Release{
								Namespace: "Required", Name: "Dependency", VersionNumber: "2.4.0",
								FullName: "Required-Dependency-2.4.0", DownloadURL: "dependency-download",
								Dependencies: []string{"Nested-NotTraversed-1.0.0"},
							}}, nil
						default:
							t.Fatalf("unexpected package lookup %s/%s", namespace, name)
							return thunderstore.Package{}, nil
						}
					},
					GetReleaseFunc: func(namespace, name, version string) (thunderstore.Release, error) {
						releaseFetches++
						if namespace != root.Namespace || name != root.Name || version != "1.0.0" {
							t.Fatalf("retry requested the wrong installed release: %s/%s/%s", namespace, name, version)
						}
						return root, nil
					},
				}
				client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
					switch url {
					case "root-download":
						rootDownloads++
						if existing || rootDownloads != 1 {
							t.Fatal("retry downloaded the installed root")
						}
						return &http.Response{Body: io.NopCloser(bytes.NewReader(modIdentityArchive(t, "root bytes")))}, nil
					case "dependency-download":
						dependencyDownloads++
						if fail && failure == "download" {
							return nil, cause
						}
						if fail && failure == "archive" {
							return &http.Response{Body: io.NopCloser(strings.NewReader("invalid archive"))}, nil
						}
						return &http.Response{Body: io.NopCloser(bytes.NewReader(modIdentityArchive(t, "dependency bytes")))}, nil
					default:
						t.Fatalf("unexpected download %q", url)
						return nil, cause
					}
				}}
				wantFiles, wantRecords := f.originalFiles(), []mod.Mod{f.original, f.other}
				if !existing {
					wantFiles["BepInEx/plugins/Owned-Root-1.0.0/plugin.dll"] = "root bytes"
					wantRecords = append(wantRecords, mod.Mod{
						ID: 3, Namespace: "Owned", Name: "Root", Version: "1.0.0",
						FilePath:   filepath.Join(f.server, "BepInEx/plugins/Owned-Root-1.0.0"),
						WebsiteURL: "https://example.invalid/root", Description: "root metadata",
					})
				}
				// Each request reloads the real records through a new service instance.
				add := func() error {
					return service.NewModService(f.records, file.NewManager(client, f.server), ts, strings.NewReader("")).AddMod(root.Namespace, root.Name)
				}
				for attempt := 0; attempt < 2; attempt++ {
					err := add()
					wantCause := cause
					if failure == "archive" {
						wantCause = file.ErrZipReadFailed
					}
					if !errors.Is(err, service.ErrAddDependenciesFailed) || !errors.Is(err, wantCause) {
						t.Fatalf("attempt %d: error = %v, want dependency failure retaining %v", attempt+1, err, wantCause)
					}
					if _, completed := err.(*service.ModMutationCleanupError); completed {
						t.Errorf("dependency failure reported whole-request completion: %v", err)
					}
					assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
					f.assertNoArtifacts(t)
				}
				fail = false
				if err := add(); err != nil {
					t.Fatalf("retry did not repair the direct dependency: %v", err)
				}
				wantFiles["BepInEx/plugins/Required-Dependency-2.4.0/plugin.dll"] = "dependency bytes"
				wantRecords = append(wantRecords, mod.Mod{
					ID: len(wantRecords) + 1, Namespace: "Required", Name: "Dependency", Version: "2.4.0",
					FilePath: filepath.Join(f.server, "BepInEx/plugins/Required-Dependency-2.4.0"),
				})
				assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
				if err := add(); !errors.Is(err, service.ErrModAlreadyInstalled) {
					t.Errorf("healthy repeat = %v, want already installed", err)
				}
				assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
				f.assertNoArtifacts(t)
				wantRoot, wantRelease, wantDownloads := 1, 3, 3
				if existing {
					wantRoot, wantRelease = 0, 4
				}
				if failure == "lookup" {
					wantDownloads = 1
				}
				if rootFetches != wantRoot || rootDownloads != wantRoot || releaseFetches != wantRelease ||
					dependencyFetches != 3 || dependencyDownloads != wantDownloads {
					t.Errorf("root fetches/downloads=%d/%d release fetches=%d dependency fetches/downloads=%d/%d; want %d/%d, %d, 3/%d",
						rootFetches, rootDownloads, releaseFetches, dependencyFetches, dependencyDownloads, wantRoot, wantRoot, wantRelease, wantDownloads)
				}
			})
		}
	}
}
