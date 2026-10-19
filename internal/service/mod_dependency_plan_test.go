package service_test

import (
	"bytes"
	"cmp"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/mock"
)

func TestDependencyMetadata_RejectsBeforePublication(t *testing.T) {
	tests := map[string]struct {
		tokens         []string
		invalidToken   string
		invalidVersion bool
	}{
		"malformed first token": {
			tokens: []string{"malformed", "Team_42-Dependency_9-1.0.0"}, invalidToken: "malformed",
		},
		"malformed later token": {
			tokens: []string{"Team_42-Dependency_9-1.0.0", "malformed"}, invalidToken: "malformed",
		},
		"empty token": {
			tokens: []string{""},
		},
		"missing name and version": {
			tokens: []string{"Owner"}, invalidToken: "Owner",
		},
		"missing namespace": {
			tokens: []string{"Dependency-1.0.0"}, invalidToken: "Dependency-1.0.0",
		},
		"missing name": {
			tokens: []string{"Owner-1.0.0"}, invalidToken: "Owner-1.0.0",
		},
		"missing version": {
			tokens: []string{"Owner-Dependency"}, invalidToken: "Owner-Dependency",
		},
		"empty namespace": {
			tokens: []string{"-Dependency-1.0.0"}, invalidToken: "-Dependency-1.0.0",
		},
		"empty name": {
			tokens: []string{"Owner--1.0.0"}, invalidToken: "Owner--1.0.0",
		},
		"empty version": {
			tokens: []string{"Owner-Dependency-"}, invalidToken: "Owner-Dependency-", invalidVersion: true,
		},
		"malformed version": {
			tokens: []string{"Owner-Dependency-broken"}, invalidToken: "Owner-Dependency-broken", invalidVersion: true,
		},
		"abbreviated version": {
			tokens: []string{"Owner-Dependency-1.0"}, invalidToken: "Owner-Dependency-1.0", invalidVersion: true,
		},
		"prefixed version": {
			tokens: []string{"Owner-Dependency-v1.0.0"}, invalidToken: "Owner-Dependency-v1.0.0", invalidVersion: true,
		},
		"four component version": {
			tokens: []string{"Owner-Dependency-1.0.0.1"}, invalidToken: "Owner-Dependency-1.0.0.1", invalidVersion: true,
		},
		"leading zero in core": {
			tokens: []string{"Owner-Dependency-1.01.0"}, invalidToken: "Owner-Dependency-1.01.0", invalidVersion: true,
		},
		"leading zero in numeric prerelease": {
			tokens: []string{"Owner-Dependency-1.0.0-01"}, invalidToken: "Owner-Dependency-1.0.0-01", invalidVersion: true,
		},
		"empty prerelease identifier": {
			tokens: []string{"Owner-Dependency-1.0.0-alpha..1"}, invalidToken: "Owner-Dependency-1.0.0-alpha..1", invalidVersion: true,
		},
		"empty build metadata": {
			tokens: []string{"Owner-Dependency-1.0.0+"}, invalidToken: "Owner-Dependency-1.0.0+", invalidVersion: true,
		},
		"whitespace namespace": {
			tokens: []string{" -Dependency-1.0.0"}, invalidToken: " -Dependency-1.0.0",
		},
		"whitespace name": {
			tokens: []string{"Owner- -1.0.0"}, invalidToken: "Owner- -1.0.0",
		},
		"non-ASCII namespace letter": {
			tokens: []string{"Team_42-Dependency_9-1.0.0", "T\u00e9am-Dependency-1.0.0"}, invalidToken: "T\u00e9am-Dependency-1.0.0",
		},
		"non-ASCII namespace digit": {
			tokens: []string{"Team_42-Dependency_9-1.0.0", "Team\u0663-Dependency-1.0.0"}, invalidToken: "Team\u0663-Dependency-1.0.0",
		},
		"non-ASCII name letter": {
			tokens: []string{"Team_42-Dependency_9-1.0.0", "Owner-D\u00e9pendency-1.0.0"}, invalidToken: "Owner-D\u00e9pendency-1.0.0",
		},
		"non-ASCII name digit": {
			tokens: []string{"Team_42-Dependency_9-1.0.0", "Owner-Dependency\u0663-1.0.0"}, invalidToken: "Owner-Dependency\u0663-1.0.0",
		},
		"namespace outside the Thunderstore alphabet": {
			tokens: []string{"Owner/Team-Dependency-1.0.0"}, invalidToken: "Owner/Team-Dependency-1.0.0",
		},
		"name outside the Thunderstore alphabet": {
			tokens: []string{"Owner-Dependency.Name-1.0.0"}, invalidToken: "Owner-Dependency.Name-1.0.0",
		},
		"whitespace version": {
			tokens: []string{"Owner-Dependency- 1.0.0"}, invalidToken: "Owner-Dependency- 1.0.0", invalidVersion: true,
		},
		"malformed framework version": {
			tokens: []string{"denikson-BepInExPack_Valheim-broken"}, invalidToken: "denikson-BepInExPack_Valheim-broken", invalidVersion: true,
		},
	}
	for _, operation := range []string{"add", "single", "qualified", "all"} {
		t.Run(operation, func(t *testing.T) {
			for name, tt := range tests {
				t.Run("given "+name+", when mutating a root, then reject before publication", func(t *testing.T) {
					f := newModReplacementFixture(t)
					root := dependencyRoot(f, operation)
					root.Dependencies = slices.Clone(tt.tokens)
					lookups := []string{}
					ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
						lookups = append(lookups, namespace+"/"+name)
						if namespace == root.Namespace && name == root.Name {
							return thunderstore.Package{Latest: root}, nil
						}
						if namespace == "Team_42" && name == "Dependency_9" {
							return thunderstore.Package{Latest: thunderstore.Release{
								Namespace: "Team_42", Name: "Dependency_9", VersionNumber: "9.2.0",
								FullName: "Team_42-Dependency_9-9.2.0", DownloadURL: "dependency-download",
							}}, nil
						}
						t.Fatalf("unexpected package fetch %s/%s", namespace, name)
						return thunderstore.Package{}, nil
					}}
					ms, calls := dependencyMutationService(t, f, ts, strings.NewReader("Y\n"))
					t.Cleanup(func() {
						assertModReplacementState(t, f.server, f.records, f.originalFiles(), []mod.Mod{f.original, f.other})
						f.assertNoArtifacts(t)
						if calls.mutations != 0 || len(calls.downloads) != 0 {
							t.Errorf("invalid input reached mutation boundaries: calls=%d downloads=%v", calls.mutations, calls.downloads)
						}
						if !slices.Equal(lookups, []string{root.Namespace + "/" + root.Name}) {
							t.Errorf("package fetches = %v, want only the attempted root", lookups)
						}
					})
					err := runDependencyMutation(ms, operation)
					if !errors.Is(err, service.ErrAddDependenciesFailed) || !errors.Is(err, service.ErrInvalidDependency) {
						t.Fatalf("error = %v, want public dependency failure", err)
					}
					if errors.Is(err, service.ErrInvalidReleaseVersion) != tt.invalidVersion {
						t.Errorf("version cause = %v, want invalid version %v", err, tt.invalidVersion)
					}
					for _, context := range []string{root.Namespace + "/" + root.Name, `"` + tt.invalidToken + `"`} {
						if !strings.Contains(err.Error(), context) {
							t.Errorf("dependency error omits %q: %v", context, err)
						}
					}
					if _, completed := err.(*service.ModMutationCleanupError); completed {
						t.Errorf("rejected dependency metadata reported completion: %v", err)
					}
					if !slices.Equal(root.Dependencies, tt.tokens) {
						t.Errorf("validation changed caller-owned tokens: %v", root.Dependencies)
					}
				})
			}
		})
	}
}

func TestDependencyMetadata_InstallsLatestDirectDependencies(t *testing.T) {
	tests := map[string]struct {
		tokens        []string
		hasDependency bool
	}{
		"nil dependencies": {},
		"empty dependencies": {
			tokens: []string{},
		},
		"framework only": {
			tokens: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
		},
		"an ordinary dependency and framework": {
			tokens: []string{"Team_42-Dependency_9-1.0.0", "denikson-BepInExPack_Valheim-5.4.2202"}, hasDependency: true,
		},
		"hyphens in a prerelease": {
			tokens: []string{"Team_42-Dependency_9-1.2.3-alpha-beta.10"}, hasDependency: true,
		},
		"hyphens and leading zeroes in build metadata": {
			tokens: []string{"Team_42-Dependency_9-1.2.3+linux-x64.001"}, hasDependency: true,
		},
		"prerelease and build metadata together": {
			tokens: []string{"Team_42-Dependency_9-1.2.3-rc-1.10+build-2.001"}, hasDependency: true,
		},
	}
	for _, operation := range []string{"add", "single", "qualified", "all"} {
		t.Run(operation, func(t *testing.T) {
			for name, tt := range tests {
				t.Run("given "+name+", when mutating a root, then keep latest direct selection", func(t *testing.T) {
					f := newModReplacementFixture(t)
					root := dependencyRoot(f, operation)
					root.Dependencies = slices.Clone(tt.tokens)
					var lookups []string
					ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
						lookups = append(lookups, namespace+"/"+name)
						switch {
						case namespace == root.Namespace && name == root.Name:
							return thunderstore.Package{Latest: root}, nil
						case namespace == "Team_42" && name == "Dependency_9":
							return thunderstore.Package{Latest: thunderstore.Release{
								Namespace: "Team_42", Name: "Dependency_9", VersionNumber: "9.2.0",
								FullName: "Team_42-Dependency_9-9.2.0", DownloadURL: "dependency-download",
								Dependencies: []string{"Nested-NotTraversed-1.0.0"},
							}}, nil
						case namespace == f.other.Namespace && name == f.other.Name:
							return thunderstore.Package{Latest: thunderstore.Release{VersionNumber: "3.0.0"}}, nil
						default:
							t.Fatalf("unexpected package fetch %s/%s", namespace, name)
							return thunderstore.Package{}, nil
						}
					}}
					ms, calls := dependencyMutationService(t, f, ts, strings.NewReader("Y\n"))
					if err := runDependencyMutation(ms, operation); err != nil {
						t.Fatal(err)
					}
					wantFiles, wantRecords := f.updatedFiles(), []mod.Mod{f.updatedRecord(), f.other}
					dependencyID := 3
					if operation == "add" {
						wantFiles, wantRecords = f.originalFiles(), []mod.Mod{f.original, f.other, {
							ID: 3, Namespace: "Owned", Name: "Root", Version: "2.0.0",
							FilePath:   filepath.Join(f.server, "BepInEx/plugins/Owned-Root-2.0.0"),
							WebsiteURL: "https://example.invalid/new", Description: "replacement metadata",
						}}
						wantFiles["BepInEx/plugins/Owned-Root-2.0.0/plugin.dll"] = "replacement DLL"
						wantFiles["BepInEx/plugins/Owned-Root-2.0.0/nested/new-data"] = "replacement data"
						dependencyID = 4
					}
					wantLookups := []string{root.Namespace + "/" + root.Name}
					wantDownloads := []string{"https://example.invalid/replacement"}
					if tt.hasDependency {
						wantFiles["BepInEx/plugins/Team_42-Dependency_9-9.2.0/plugin.dll"] = "replacement DLL"
						wantFiles["BepInEx/plugins/Team_42-Dependency_9-9.2.0/nested/new-data"] = "replacement data"
						wantRecords = append(wantRecords, mod.Mod{
							ID: dependencyID, Namespace: "Team_42", Name: "Dependency_9", Version: "9.2.0",
							FilePath: filepath.Join(f.server, "BepInEx/plugins/Team_42-Dependency_9-9.2.0"),
						})
						wantLookups = append(wantLookups, "Team_42/Dependency_9")
						wantDownloads = append(wantDownloads, "dependency-download")
					}
					if operation == "all" {
						wantLookups = append(wantLookups, "Other/Unrelated")
					}
					if !slices.Equal(lookups, wantLookups) || !slices.Equal(calls.downloads, wantDownloads) {
						t.Errorf("package fetches/downloads = %v/%v, want %v/%v", lookups, calls.downloads, wantLookups, wantDownloads)
					}
					if !slices.Equal(root.Dependencies, tt.tokens) {
						t.Errorf("request changed declared tokens: %v", root.Dependencies)
					}
					assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
					f.assertNoArtifacts(t)
				})
			}
		})
	}
}

func TestDependencyMetadata_DoesNotOverrideUpdateGates(t *testing.T) {
	readerCause := errors.New("confirmation reader interrupted")
	tests := map[string]struct {
		latest, answer string
		noUpdate       bool
		readerFailure  bool
		wantErr        error
	}{
		"an equal version": {
			latest: "1.0.0", answer: "Y\n", noUpdate: true, wantErr: service.ErrNoChanges,
		},
		"an older version": {
			latest: "0.9.0", answer: "Y\n", noUpdate: true, wantErr: service.ErrNoChanges,
		},
		"only a build metadata difference": {
			latest: "1.0.0+different.1", answer: "Y\n", noUpdate: true, wantErr: service.ErrNoChanges,
		},
		"a declined update": {
			latest: "2.0.0", answer: "n\n", wantErr: service.ErrCancelled,
		},
		"EOF at confirmation": {
			latest: "2.0.0", wantErr: io.EOF,
		},
		"exhausted confirmation attempts": {
			latest: "2.0.0", answer: "invalid\ninvalid\nY\n", wantErr: service.ErrMaxAttempts,
		},
		"a failed confirmation reader": {
			latest: "2.0.0", readerFailure: true, wantErr: readerCause,
		},
		"an invalid latest version": {
			latest: "broken", answer: "Y\n", wantErr: service.ErrInvalidReleaseVersion,
		},
	}
	for _, operation := range []string{"single", "qualified", "all"} {
		t.Run(operation, func(t *testing.T) {
			for name, tt := range tests {
				t.Run("given malformed dependencies and "+name+", when updating, then preserve the earlier outcome", func(t *testing.T) {
					f := newModReplacementFixture(t)
					root := f.latest
					root.VersionNumber, root.FullName = tt.latest, "Owned-Installed-"+tt.latest
					root.Dependencies = []string{"malformed"}
					var lookups []string
					ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
						lookups = append(lookups, namespace+"/"+name)
						if namespace == "Owned" && name == "Installed" {
							return thunderstore.Package{Latest: root}, nil
						}
						if namespace == "Other" && name == "Unrelated" {
							return thunderstore.Package{Latest: thunderstore.Release{VersionNumber: "3.0.0"}}, nil
						}
						t.Fatalf("unexpected package fetch %s/%s", namespace, name)
						return thunderstore.Package{}, nil
					}}
					var in io.Reader = strings.NewReader(tt.answer)
					if tt.readerFailure || tt.noUpdate && operation != "all" {
						in = iotest.ErrReader(readerCause)
					}
					ms, calls := dependencyMutationService(t, f, ts, in)
					err := runModReplacement(ms, operation)
					if !errors.Is(err, tt.wantErr) {
						t.Errorf("earlier outcome = %v, want %v", err, tt.wantErr)
					}
					if (tt.wantErr == io.EOF || tt.readerFailure) && !errors.Is(err, service.ErrConfirmationInput) {
						t.Errorf("confirmation cause lost: %v", err)
					}
					if tt.wantErr == service.ErrInvalidReleaseVersion && !errors.Is(err, service.ErrUnableToUpdateMod) {
						t.Errorf("release version failure lost its public classification: %v", err)
					}
					if errors.Is(err, service.ErrInvalidDependency) || errors.Is(err, service.ErrAddDependenciesFailed) {
						t.Errorf("dependency validation overrode the earlier gate: %v", err)
					}
					wantLookups := []string{"Owned/Installed"}
					if operation == "all" {
						if tt.answer != "Y\n" {
							wantLookups = nil
						} else if tt.noUpdate {
							wantLookups = append(wantLookups, "Other/Unrelated")
						}
					}
					if !slices.Equal(lookups, wantLookups) || calls.mutations != 0 || len(calls.downloads) != 0 {
						t.Errorf("unexpected work after update gate: fetches=%v, mutations=%d, downloads=%v", lookups, calls.mutations, calls.downloads)
					}
					assertModReplacementState(t, f.server, f.records, f.originalFiles(), []mod.Mod{f.original, f.other})
					f.assertNoArtifacts(t)
				})
			}
		})
	}
}

type dependencyMutationCalls struct {
	mutations int
	downloads []string
}

func dependencyMutationService(t *testing.T, f modReplacementFixture, ts thunderstore.Thunderstore, in io.Reader) (service.Mod, *dependencyMutationCalls) {
	t.Helper()
	calls := &dependencyMutationCalls{}
	payload := replacementArchive(t, "success")
	manager := file.NewManager(&mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
		calls.downloads = append(calls.downloads, url)
		return &http.Response{Body: io.NopCloser(bytes.NewReader(payload))}, nil
	}}, f.server)
	fm := &mock.Manager{
		ValidateModInstallFunc: manager.ValidateModInstall,
		InstallModFunc: func(url, fullName string) (string, error) {
			calls.mutations++
			return manager.InstallMod(url, fullName)
		},
		ReplaceModFunc: func(url, oldFullName, newFullName string) (file.ModReplacement, error) {
			calls.mutations++
			return manager.ReplaceMod(url, oldFullName, newFullName)
		},
	}
	r := &mock.ModsRepo{
		GetModFunc: f.records.GetMod, GetModByIdentityFunc: f.records.GetModByIdentity,
		ListModsFunc: func() ([]mod.Mod, error) {
			records, err := f.records.ListMods()
			slices.SortFunc(records, func(a, b mod.Mod) int { return cmp.Compare(a.ID, b.ID) })
			return records, err
		},
		UpsertModFunc: func(m mod.Mod) error {
			calls.mutations++
			return f.records.UpsertMod(m)
		},
	}
	return service.NewModService(r, fm, ts, in), calls
}

func dependencyRoot(f modReplacementFixture, operation string) thunderstore.Release {
	root := f.latest
	if operation == "add" {
		root.Name, root.FullName = "Root", "Owned-Root-2.0.0"
	}
	return root
}

func runDependencyMutation(ms service.Mod, operation string) error {
	if operation == "add" {
		return ms.AddMod("Owned", "Root")
	}
	return runModReplacement(ms, operation)
}
