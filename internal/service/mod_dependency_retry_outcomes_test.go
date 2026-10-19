package service_test

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"warden/command"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/mock"
)

func TestAddMod_RetryCleanupOutcomes(t *testing.T) {
	for name, tt := range map[string]struct {
		failure                                string
		fetched, downloaded, written, recorded []string
		laterFiles                             bool
		retryCalls                             []string
	}{
		"remaining dependencies succeed": {
			fetched: []string{"First", "Later"}, downloaded: []string{"First", "Later"},
			written: []string{"First", "Later"}, recorded: []string{"First", "Later"}, laterFiles: true,
		},
		"a later package lookup fails": {
			failure: "lookup", fetched: []string{"First", "Later"}, downloaded: []string{"First"},
			written: []string{"First"}, recorded: []string{"First"},
		},
		"a later archive fails": {
			failure: "files", fetched: []string{"First", "Later"}, downloaded: []string{"First", "Later"},
			written: []string{"First"}, recorded: []string{"First"},
		},
		"a later metadata write is rejected": {
			failure: "record-later", fetched: []string{"First", "Later"}, downloaded: []string{"First", "Later"},
			written: []string{"First", "Later"}, recorded: []string{"First"}, laterFiles: true,
			retryCalls: []string{"First", "Later", "Later"},
		},
		"the warned dependency's metadata write is rejected": {
			failure: "record-first", fetched: []string{"First"}, downloaded: []string{"First"},
			written:    []string{"First"},
			retryCalls: []string{"First", "First", "Later"},
		},
		"the warned dependency's commit is not acknowledged": {
			failure: "commit-first", fetched: []string{"First"}, downloaded: []string{"First"},
			written: []string{"First"}, recorded: []string{"First"},
			retryCalls: []string{"First", "Later"},
		},
		"a later failure contains a nested completion marker": {
			failure: "nested", fetched: []string{"First", "Later"}, downloaded: []string{"First"},
			written: []string{"First"}, recorded: []string{"First"},
		},
	} {
		t.Run("given a dependency cleanup warning, when "+name+", then report the whole retry honestly", func(t *testing.T) {
			f := newModReplacementFixture(t)
			t.Setenv("HOME", t.TempDir())
			artifact := filepath.Join(t.TempDir(), "cleanup-leftover")
			cleanupCause, laterCause := errors.New("dependency cleanup interrupted"), errors.New("dependency work interrupted")
			cleanup := &file.ModInstallCleanupError{Err: fmt.Errorf("%w at %q: %w", file.ErrBackupDeleteFailed, artifact, cleanupCause)}
			if strings.HasPrefix(tt.failure, "record-") {
				target := "First"
				if tt.failure == "record-later" {
					target = "Later"
				}
				if _, err := f.db.(*sql.DB).Exec(fmt.Sprintf(`CREATE TRIGGER reject_dependency BEFORE INSERT ON mods
					WHEN NEW.namespace = 'Required' AND NEW.name = '%s'
					BEGIN SELECT RAISE(FAIL, 'dependency metadata rejected'); END`, target)); err != nil {
					t.Fatal(err)
				}
			}
			var fetched, downloaded, written []string
			ts := &mock.Thunderstore{
				GetReleaseFunc: func(namespace, name, version string) (thunderstore.Release, error) {
					if namespace != "Owned" || name != "Installed" || version != "1.0.0" {
						t.Fatalf("wrong installed release %s/%s/%s", namespace, name, version)
					}
					return thunderstore.Release{
						Namespace: namespace, Name: name, VersionNumber: version, FullName: "Owned-Installed-1.0.0",
						Dependencies: []string{"Required-First-1.0.0", "Other-Unrelated-1.0.0", "Required-Later-1.0.0"},
					}, nil
				},
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					if namespace != "Required" || (name != "First" && name != "Later") {
						t.Fatalf("unexpected root, healthy dependency, or recursive lookup %s/%s", namespace, name)
					}
					fetched = append(fetched, name)
					if name == "Later" {
						switch tt.failure {
						case "lookup":
							return thunderstore.Package{}, laterCause
						case "nested":
							return thunderstore.Package{}, errors.Join(laterCause, &service.ModMutationCleanupError{Err: errors.New("nested marker")})
						}
					}
					return thunderstore.Package{Latest: thunderstore.Release{
						Namespace: namespace, Name: name, VersionNumber: "2.0.0", FullName: "Required-" + name + "-2.0.0",
						DownloadURL: name, Dependencies: []string{"Nested-NotTraversed-1.0.0"},
					}}, nil
				},
			}
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				if url != "First" && url != "Later" {
					t.Fatalf("unexpected download %q", url)
				}
				downloaded = append(downloaded, url)
				if url == "Later" && tt.failure == "files" {
					return &http.Response{Body: io.NopCloser(strings.NewReader("invalid archive"))}, nil
				}
				return &http.Response{Body: io.NopCloser(bytes.NewReader(modIdentityArchive(t, url+" bytes")))}, nil
			}}
			manager := file.NewManager(client, f.server)
			warnFirst := true
			fm := &mock.Manager{InstallModFunc: func(url, fullName string) (string, error) {
				path, err := manager.InstallMod(url, fullName)
				if err == nil && url == "First" && warnFirst {
					if err := os.WriteFile(artifact, []byte("partial cleanup artifact"), 0o600); err != nil {
						t.Fatal(err)
					}
					return path, cleanup
				}
				return path, err
			}}
			r := &mock.ModsRepo{
				GetModByIdentityFunc: f.records.GetModByIdentity,
				UpsertModFunc: func(m mod.Mod) error {
					if m.Namespace != "Required" || (m.Name != "First" && m.Name != "Later") || m.Version != "2.0.0" {
						t.Fatalf("retry overwrote an existing record or chose a pinned version: %+v", m)
					}
					written = append(written, m.Name)
					if err := f.records.UpsertMod(m); err != nil {
						return err
					}
					if tt.failure == "commit-first" && m.Name == "First" {
						return laterCause
					}
					return nil
				},
			}
			svc := service.NewModService(r, fm, ts, strings.NewReader(""))
			observed := &observedRetryAdd{Mod: svc}
			fs := service.NewFrameworkService(repo.NewFrameworksRepo(f.db), manager, ts, strings.NewReader(""))
			cmd := command.NewAddCommand(fs, observed)
			output, err := os.CreateTemp(t.TempDir(), "add-output")
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, args := os.Stdout, os.Stderr, os.Args
			t.Cleanup(func() {
				os.Stdout, os.Stderr, os.Args = stdout, stderr, args
				if err := output.Close(); err != nil {
					t.Error(err)
				}
			})
			os.Stdout, os.Stderr = output, output
			os.Args = []string{"warden", "add", "--namespace", "Owned", "--mod", "Installed"}
			commandErr := command.Execute(cmd)
			os.Stdout, os.Stderr, os.Args = stdout, stderr, args
			err = observed.result
			data, readErr := os.ReadFile(output.Name())
			if readErr != nil {
				t.Fatal(readErr)
			}
			completed := tt.failure == ""
			wantCommandErr, wantErrors := err, 1
			if completed {
				wantCommandErr, wantErrors = nil, 0
			}
			if !errors.Is(commandErr, wantCommandErr) || strings.Count(string(data), "Error:") != wantErrors ||
				strings.Contains(string(data), "Usage:") {
				t.Errorf("command lost or duplicated the whole-request outcome: error=%v, output=%q", commandErr, data)
			}
			if _, direct := err.(*service.ModMutationCleanupError); direct != completed {
				t.Errorf("whole-request completion = %v, want %v: %v", direct, completed, err)
			}
			if !errors.Is(err, cleanupCause) || !errors.Is(err, file.ErrBackupDeleteFailed) {
				t.Errorf("earlier cleanup diagnostics were lost: %v", err)
			}
			if !completed {
				var fileCleanup *file.ModInstallCleanupError
				if !errors.As(commandErr, &fileCleanup) || fileCleanup != cleanup {
					t.Errorf("command lost the inspectable cleanup cause: %v", commandErr)
				}
				wantCause := laterCause
				if tt.failure == "files" {
					wantCause = file.ErrZipReadFailed
				} else if strings.HasPrefix(tt.failure, "record-") {
					wantCause = repo.ErrModInsertFailed
				}
				if !errors.Is(err, service.ErrAddDependenciesFailed) || !errors.Is(err, wantCause) {
					t.Errorf("incomplete retry lost its failure: %v", err)
				}
			}
			if strings.HasPrefix(tt.failure, "record-") || tt.failure == "commit-first" {
				if !strings.Contains(err.Error(), "verify database state before retrying") {
					t.Errorf("metadata uncertainty was hidden: %v", err)
				}
			}
			for _, text := range []string{"keeping installed Owned-Installed-1.0.0", cleanupCause.Error(), artifact} {
				if strings.Count(string(data), text) != 1 {
					t.Errorf("diagnostic %q must appear once in %q", text, data)
				}
			}
			if strings.Contains(string(data), "... warning:") != completed || strings.Contains(string(data), "successfully") {
				t.Errorf("command misreported the retry outcome: %q", data)
			}
			wantFiles, wantRecords := f.originalFiles(), []mod.Mod{f.original, f.other}
			wantFiles["BepInEx/plugins/Required-First-2.0.0/plugin.dll"] = "First bytes"
			if tt.laterFiles {
				wantFiles["BepInEx/plugins/Required-Later-2.0.0/plugin.dll"] = "Later bytes"
			}
			for i, name := range tt.recorded {
				wantRecords = append(wantRecords, mod.Mod{
					ID: 3 + i, Namespace: "Required", Name: name, Version: "2.0.0",
					FilePath: filepath.Join(f.server, "BepInEx/plugins/Required-"+name+"-2.0.0"),
				})
			}
			assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
			if !slices.Equal(fetched, tt.fetched) || !slices.Equal(downloaded, tt.downloaded) || !slices.Equal(written, tt.written) {
				t.Errorf("fetched/downloaded/written = %v/%v/%v, want %v/%v/%v", fetched, downloaded, written, tt.fetched, tt.downloaded, tt.written)
			}
			if data, err := os.ReadFile(artifact); err != nil || string(data) != "partial cleanup artifact" {
				t.Errorf("cleanup artifact was lost or misrepresented: %q, %v", data, err)
			}
			if len(tt.retryCalls) > 0 {
				if strings.HasPrefix(tt.failure, "record-") {
					if _, err := f.db.(*sql.DB).Exec(`DROP TRIGGER reject_dependency`); err != nil {
						t.Fatal(err)
					}
				}
				warnFirst = false
				if err := svc.AddMod("Owned", "Installed"); err != nil {
					t.Fatalf("retry after verifying metadata state and resolving the failure failed: %v", err)
				}
				wantFiles["BepInEx/plugins/Required-Later-2.0.0/plugin.dll"] = "Later bytes"
				wantRecords = []mod.Mod{f.original, f.other, {
					ID: 3, Namespace: "Required", Name: "First", Version: "2.0.0",
					FilePath: filepath.Join(f.server, "BepInEx/plugins/Required-First-2.0.0"),
				}, {
					ID: 4, Namespace: "Required", Name: "Later", Version: "2.0.0",
					FilePath: filepath.Join(f.server, "BepInEx/plugins/Required-Later-2.0.0"),
				}}
				assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
				if err := svc.AddMod("Owned", "Installed"); !errors.Is(err, service.ErrModAlreadyInstalled) {
					t.Errorf("healthy repeat after metadata recovery = %v, want already installed", err)
				}
				assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
				for _, calls := range [][]string{fetched, downloaded, written} {
					if !slices.Equal(calls, tt.retryCalls) {
						t.Errorf("retry repeated recorded work or skipped a rejected write: %v, want %v", calls, tt.retryCalls)
					}
				}
			}
			f.assertNoArtifacts(t)
		})
	}
}

func TestAddMod_FreshRootCleanupLookupOutcomes(t *testing.T) {
	queryCause := errors.New("dependency identity query interrupted")
	for name, lookupErr := range map[string]error{
		"a dependency query fails":      fmt.Errorf("%w: %w", repo.ErrModFetchFailed, queryCause),
		"a dependency is ambiguous":     repo.ErrModFetchMultipleResults,
		"all dependencies are recorded": nil,
	} {
		t.Run("given a new root with a cleanup warning, when "+name+", then preserve its publication and the request outcome", func(t *testing.T) {
			f := newModReplacementFixture(t)
			root := thunderstore.Release{
				Namespace: "Owned", Name: "NewRoot", VersionNumber: "2.0.0", FullName: "Owned-NewRoot-2.0.0",
				DownloadURL: "root-download", WebsiteURL: "https://example.invalid/root", Description: "new root metadata",
				Dependencies: []string{"Other-Unrelated-1.0.0"},
			}
			artifact := filepath.Join(t.TempDir(), "cleanup-leftover")
			cleanupCause := errors.New("root cleanup interrupted")
			warning := &file.ModInstallCleanupError{Err: fmt.Errorf("%w at %q: %w", file.ErrBackupDeleteFailed, artifact, cleanupCause)}
			packages, downloads, writes, releases := 0, 0, 0, 0
			failLookup := lookupErr != nil
			r := &mock.ModsRepo{
				GetModByIdentityFunc: func(namespace, name string) (mod.Mod, error) {
					if namespace == "Other" && name == "Unrelated" {
						if writes != 1 {
							t.Fatal("dependency lookup did not follow root publication and recording")
						}
						if failLookup {
							return mod.Mod{}, lookupErr
						}
					} else if namespace != root.Namespace || name != root.Name {
						t.Fatalf("unexpected identity lookup %s/%s", namespace, name)
					}
					return f.records.GetModByIdentity(namespace, name)
				},
				UpsertModFunc: func(m mod.Mod) error {
					writes++
					if m.Namespace != root.Namespace || m.Name != root.Name || writes != 1 {
						t.Fatalf("unexpected record mutation: %+v", m)
					}
					return f.records.UpsertMod(m)
				},
			}
			ts := &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					packages++
					if namespace != root.Namespace || name != root.Name || packages != 1 {
						t.Fatalf("dependency or repeated root fetch: %s/%s", namespace, name)
					}
					return thunderstore.Package{Latest: root}, nil
				},
				GetReleaseFunc: func(namespace, name, version string) (thunderstore.Release, error) {
					releases++
					if namespace != root.Namespace || name != root.Name || version != root.VersionNumber {
						t.Fatalf("wrong installed release: %s/%s/%s", namespace, name, version)
					}
					return root, nil
				},
			}
			manager := file.NewManager(&mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				downloads++
				if url != root.DownloadURL || downloads != 1 {
					t.Fatalf("dependency or repeated root download: %q", url)
				}
				return &http.Response{Body: io.NopCloser(bytes.NewReader(modIdentityArchive(t, "new root bytes")))}, nil
			}}, f.server)
			fm := &mock.Manager{InstallModFunc: func(url, fullName string) (string, error) {
				if url != root.DownloadURL || fullName != root.FullName {
					t.Fatalf("unexpected file mutation: %q/%q", url, fullName)
				}
				path, err := manager.InstallMod(url, fullName)
				if err != nil {
					return path, err
				}
				return path, warning
			}}
			svc := service.NewModService(r, fm, ts, strings.NewReader(""))
			err := svc.AddMod(root.Namespace, root.Name)
			if !errors.Is(err, cleanupCause) || !errors.Is(err, file.ErrBackupDeleteFailed) {
				t.Fatalf("root cleanup diagnostic was lost: %v", err)
			}
			if !strings.Contains(err.Error(), artifact) {
				t.Errorf("root cleanup path was lost: %v", err)
			}
			if _, completed := err.(*service.ModMutationCleanupError); completed != (lookupErr == nil) {
				t.Errorf("whole-request completion misclassified: %v", err)
			}
			if lookupErr != nil {
				if !errors.Is(err, service.ErrAddDependenciesFailed) || !errors.Is(err, lookupErr) {
					t.Errorf("dependency failure was lost: %v", err)
				}
			} else if errors.Is(err, service.ErrAddDependenciesFailed) {
				t.Errorf("completed fresh add reported a dependency failure: %v", err)
			}
			if errors.Is(err, service.ErrModAlreadyInstalled) {
				t.Errorf("new root publication reported as already installed: %v", err)
			}
			wantFiles := f.originalFiles()
			wantFiles["BepInEx/plugins/Owned-NewRoot-2.0.0/plugin.dll"] = "new root bytes"
			wantRecords := []mod.Mod{f.original, f.other, {
				ID: 3, Namespace: "Owned", Name: "NewRoot", Version: "2.0.0",
				FilePath:   filepath.Join(f.server, "BepInEx/plugins/Owned-NewRoot-2.0.0"),
				WebsiteURL: "https://example.invalid/root", Description: "new root metadata",
			}}
			assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
			failLookup = false
			if err := svc.AddMod(root.Namespace, root.Name); !errors.Is(err, service.ErrModAlreadyInstalled) {
				t.Errorf("retry after resolving lookup failure = %v, want already installed", err)
			}
			if packages != 1 || downloads != 1 || writes != 1 || releases != 1 {
				t.Errorf("packages/downloads/writes/exact releases = %d/%d/%d/%d, want one each", packages, downloads, writes, releases)
			}
			assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
			f.assertNoArtifacts(t)
		})
	}
}

type observedRetryAdd struct {
	service.Mod
	result error
}

func (s *observedRetryAdd) AddMod(namespace, name string) error {
	s.result = s.Mod.AddMod(namespace, name)
	return s.result
}
