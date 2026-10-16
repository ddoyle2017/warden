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
		},
		"the warned dependency's metadata write is rejected": {
			failure: "record-first", fetched: []string{"First"}, downloaded: []string{"First"},
			written: []string{"First"},
		},
		"the warned dependency's commit is not acknowledged": {
			failure: "commit-first", fetched: []string{"First"}, downloaded: []string{"First"},
			written: []string{"First"}, recorded: []string{"First"},
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
			fm := &mock.Manager{InstallModFunc: func(url, fullName string) (string, error) {
				path, err := manager.InstallMod(url, fullName)
				if err == nil && url == "First" {
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
			cmd.SetArgs([]string{"--namespace", "Owned", "--mod", "Installed"})
			output, err := os.CreateTemp(t.TempDir(), "add-output")
			if err != nil {
				t.Fatal(err)
			}
			stdout := os.Stdout
			t.Cleanup(func() {
				os.Stdout = stdout
				if err := output.Close(); err != nil {
					t.Error(err)
				}
			})
			os.Stdout = output
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			os.Stdout = stdout
			err = observed.result
			data, readErr := os.ReadFile(output.Name())
			if readErr != nil {
				t.Fatal(readErr)
			}
			completed := tt.failure == ""
			if _, direct := err.(*service.ModMutationCleanupError); direct != completed {
				t.Errorf("whole-request completion = %v, want %v: %v", direct, completed, err)
			}
			if !errors.Is(err, cleanupCause) || !errors.Is(err, file.ErrBackupDeleteFailed) {
				t.Errorf("earlier cleanup diagnostics were lost: %v", err)
			}
			if !completed {
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
			if tt.failure == "commit-first" {
				if err := svc.AddMod("Owned", "Installed"); err != nil {
					t.Fatalf("retry after verifying the committed dependency failed: %v", err)
				}
				wantFiles["BepInEx/plugins/Required-Later-2.0.0/plugin.dll"] = "Later bytes"
				wantRecords = append(wantRecords, mod.Mod{
					ID: 4, Namespace: "Required", Name: "Later", Version: "2.0.0",
					FilePath: filepath.Join(f.server, "BepInEx/plugins/Required-Later-2.0.0"),
				})
				assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
				for _, calls := range [][]string{fetched, downloaded, written} {
					if !slices.Equal(calls, []string{"First", "Later"}) {
						t.Errorf("retry repeated the committed dependency: %v", calls)
					}
				}
			}
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
