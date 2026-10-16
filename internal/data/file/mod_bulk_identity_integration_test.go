package file_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
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

func TestBulkDuplicateAfterCommittedUpdate(t *testing.T) {
	for name, cleanupFails := range map[string]bool{
		"given an earlier clean update, when a duplicate is reached, then retain progress and stop before fetching it":     false,
		"given an earlier cleanup warning, when a duplicate is reached, then retain its causes and consumed artifact path": true,
	} {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			server := th.GetValheimDirectory()
			plugins := filepath.Join(server, "BepInEx", "plugins")
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			db := th.CreateDatabase()
			repo.CreateFrameworksTable(db)
			repo.CreateModsTable(db)
			stored := repo.NewModsRepo(db)
			seeds := []struct {
				record  mod.Mod
				payload string
			}{
				{mod.Mod{Namespace: "Owner", Name: "Earlier", Version: "1.0.0"}, "earlier original"},
				{mod.Mod{Namespace: "Author", Name: "Shared", Version: "1.0.0"}, "first duplicate"},
				{mod.Mod{Namespace: "Author", Name: "Shared", Version: "1.1.0"}, "second duplicate"},
				{mod.Mod{Namespace: "Owner", Name: "Later", Version: "1.0.0"}, "later original"},
			}
			wantFiles := map[string]string{
				"world.db": "unrelated world",
				filepath.Join("BepInEx", "plugins", "a-unrelated.dll"): "unrelated plugin",
			}
			for _, seed := range seeds {
				m := seed.record
				m.FilePath = filepath.Join(plugins, m.FullName())
				m.Description = seed.payload
				if err := stored.InsertMod(m); err != nil {
					t.Fatal(err)
				}
				wantFiles[filepath.Join("BepInEx", "plugins", m.FullName(), "plugin.dll")] = seed.payload
			}
			writeArchiveRecoveryFiles(t, server, wantFiles)
			ordered, err := stored.ListMods()
			if err != nil || len(ordered) != len(seeds) {
				t.Fatalf("seeded records = %+v, %v", ordered, err)
			}
			sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
			earlier := ordered[0]
			latest := thunderstore.Release{
				Namespace: "Owner", Name: "Earlier", VersionNumber: "2.0.0", FullName: "Owner-Earlier-2.0.0",
				DownloadURL: "earlier-download", Description: "updated earlier", WebsiteURL: "https://example.invalid/earlier",
			}
			archive := recoveryArchive(t, false, false)
			var fetched, downloaded, removed, installed, written []string
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				downloaded = append(downloaded, url)
				if url != latest.DownloadURL {
					t.Fatalf("downloaded a blocked package: %q", url)
				}
				return &http.Response{Body: io.NopCloser(bytes.NewReader(archive))}, nil
			}}
			manager := file.NewManager(client, server)
			b := file.NewBackup()
			cleanupCause := errors.New("snapshot cleanup interrupted")
			var snapshot string
			backup := &modRequestBackup{
				Backup: b,
				restore: func(string) error {
					t.Fatal("completed earlier update attempted restoration")
					return nil
				},
				remove: func() error {
					snapshot = *b.Path()
					if !cleanupFails {
						return b.Remove()
					}
					return file.RemoveBackupWithCleanup(b, func(path string) error {
						if b.Path() != nil {
							t.Fatal("cleanup began before consuming the backup")
						}
						if err := os.Remove(filepath.Join(path, "a-unrelated.dll")); err != nil {
							t.Fatal(err)
						}
						return cleanupCause
					})
				},
			}
			fm := &mock.Manager{
				ValidateModInstallFunc: manager.ValidateModInstall,
				RemoveModFunc: func(fullName string) error {
					removed = append(removed, fullName)
					if fullName != earlier.FullName() {
						t.Fatalf("removed a blocked package: %q", fullName)
					}
					return manager.RemoveMod(fullName)
				},
				InstallModFunc: func(url, fullName string) (string, error) {
					installed = append(installed, fullName)
					if url != latest.DownloadURL || fullName != latest.FullName {
						t.Fatalf("installed a blocked package: %q/%q", url, fullName)
					}
					return file.InstallModWithBackup(manager, backup, url, fullName)
				},
			}
			r := &mock.ModsRepo{
				// Pin the sequence independently of SQLite's unspecified row ordering.
				ListModsFunc: func() ([]mod.Mod, error) { return slices.Clone(ordered), nil },
				UpsertModFunc: func(m mod.Mod) error {
					identity := m.Namespace + "/" + m.Name
					written = append(written, identity)
					if identity != "Owner/Earlier" {
						t.Fatalf("wrote a blocked package: %q", identity)
					}
					return stored.UpsertMod(m)
				},
				DeleteModFunc: func(string, string) error {
					t.Fatal("duplicate rejection attempted obsolete-record compensation")
					return nil
				},
			}
			ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
				identity := namespace + "/" + name
				fetched = append(fetched, identity)
				if identity != "Owner/Earlier" {
					t.Fatalf("fetched a blocked package: %q", identity)
				}
				return thunderstore.Package{Latest: latest}, nil
			}}

			err = service.NewModService(r, fm, ts, strings.NewReader("Y\n")).UpdateAllMods()
			for _, cause := range []error{service.ErrUnableToUpdateMod, service.ErrDuplicateModRecords, repo.ErrModFetchMultipleResults} {
				if !errors.Is(err, cause) {
					t.Errorf("duplicate error lost %v: %v", cause, err)
				}
			}
			if err == nil || !strings.Contains(err.Error(), "Author/Shared") {
				t.Errorf("duplicate identity missing: %v", err)
			}
			if _, completed := err.(*service.ModMutationCleanupError); completed {
				t.Errorf("incomplete bulk update reported completion: %v", err)
			}
			var completedFiles *file.ModInstallCleanupError
			if errors.As(err, &completedFiles) != cleanupFails ||
				errors.Is(err, cleanupCause) != cleanupFails || errors.Is(err, file.ErrBackupDeleteFailed) != cleanupFails {
				t.Errorf("earlier cleanup causes lost or invented: %v", err)
			}
			if snapshot == "" || b.Path() != nil || !errors.Is(b.Restore(plugins), file.ErrBackupMissing) {
				t.Fatal("earlier update did not consume its backup")
			}
			if cleanupFails {
				if err == nil || !strings.Contains(err.Error(), snapshot) || strings.Contains(err.Error(), "backup retained at") {
					t.Errorf("consumed cleanup artifact path lost or advertised as retryable: %v", err)
				}
				assertArchiveRecoveryFiles(t, th, snapshot, map[string]string{
					filepath.Join("Author-Shared-1.0.0", "plugin.dll"): "first duplicate",
					filepath.Join("Author-Shared-1.1.0", "plugin.dll"): "second duplicate",
					filepath.Join("Owner-Later-1.0.0", "plugin.dll"):   "later original",
				}, nil)
			} else if _, statErr := os.Stat(snapshot); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("successful cleanup left its snapshot: %v", statErr)
			}
			for _, calls := range []struct {
				name      string
				got, want []string
			}{
				{"fetches", fetched, []string{"Owner/Earlier"}},
				{"downloads", downloaded, []string{latest.DownloadURL}},
				{"removals", removed, []string{"Owner-Earlier-1.0.0"}},
				{"installs", installed, []string{latest.FullName}},
				{"record writes", written, []string{"Owner/Earlier"}},
			} {
				if !slices.Equal(calls.got, calls.want) {
					t.Errorf("%s = %v, want %v", calls.name, calls.got, calls.want)
				}
			}
			wantRecords := slices.Clone(ordered)
			wantRecords[0].Version = "2.0.0"
			wantRecords[0].FilePath = filepath.Join(plugins, latest.FullName)
			wantRecords[0].WebsiteURL = latest.WebsiteURL
			wantRecords[0].Description = latest.Description
			actual, listErr := stored.ListMods()
			if listErr != nil {
				t.Fatal(listErr)
			}
			sort.Slice(actual, func(i, j int) bool { return actual[i].ID < actual[j].ID })
			if !reflect.DeepEqual(actual, wantRecords) {
				t.Errorf("records = %+v, want %+v", actual, wantRecords)
			}
			delete(wantFiles, filepath.Join("BepInEx", "plugins", "Owner-Earlier-1.0.0", "plugin.dll"))
			wantFiles[filepath.Join("BepInEx", "plugins", latest.FullName, "plugin.dll")] = "replacement plugin"
			assertArchiveRecoveryFiles(t, th, server, wantFiles, nil)
			wantBackups := 0
			if cleanupFails {
				wantBackups = 1
			}
			if entries, readErr := os.ReadDir(backups); readErr != nil || len(entries) != wantBackups {
				t.Errorf("backup artifacts = %v, %v; want %d", entries, readErr, wantBackups)
			}
		})
	}
}
