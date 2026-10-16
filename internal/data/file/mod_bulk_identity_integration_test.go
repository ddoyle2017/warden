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
	for name, tt := range map[string]struct {
		cleanupFails      bool
		earlyDependency   bool
		dependencyWarning bool
	}{
		"given an earlier clean update, when a duplicate is reached, then retain progress and stop before fetching it": {},
		"given an earlier cleanup warning, when a duplicate is reached, then retain its causes and consumed artifact path": {
			cleanupFails: true,
		},
		"given an earlier clean update, when its selected dependency is duplicated, then stop before the dependency fetch": {
			earlyDependency: true,
		},
		"given an earlier cleanup warning, when its selected dependency is duplicated, then retain cleanup causes without fetching the dependency": {
			cleanupFails: true, earlyDependency: true,
		},
		"given a completed dependency cleanup warning, when the next dependency is duplicated, then preserve both dependency outcomes": {
			cleanupFails: true, earlyDependency: true, dependencyWarning: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			cleanupFails := tt.cleanupFails
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
			cleanupName, settings := "Earlier", "earlier settings"
			if tt.dependencyWarning {
				seeds = append(seeds, struct {
					record  mod.Mod
					payload string
				}{mod.Mod{Namespace: "Owner", Name: "Support", Version: "1.0.0"}, "support original"})
				cleanupName, settings = "Support", "support settings"
			}
			wantFiles := map[string]string{
				"world.db": "unrelated world",
				filepath.Join("BepInEx", "plugins", "a-unrelated.dll"):                         "unrelated plugin",
				filepath.Join("BepInEx", "plugins", "Owner-"+cleanupName+"-1.0.0", "settings"): settings,
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
			latest := thunderstore.Release{
				Namespace: "Owner", Name: "Earlier", VersionNumber: "2.0.0", FullName: "Owner-Earlier-2.0.0",
				DownloadURL: "earlier-download", Description: "updated earlier", WebsiteURL: "https://example.invalid/earlier",
			}
			if tt.earlyDependency {
				latest.Dependencies = []string{"Author-Shared-1.0.0"}
			}
			releases := map[string]thunderstore.Release{}
			wantIdentities := []string{"Owner/Earlier"}
			wantDownloads := []string{latest.DownloadURL}
			wantFrom, wantTo := []string{"Owner-Earlier-1.0.0"}, []string{latest.FullName}
			if tt.dependencyWarning {
				latest.Dependencies = append([]string{"Owner-Support-1.0.0"}, latest.Dependencies...)
				releases["Owner/Support"] = thunderstore.Release{
					Namespace: "Owner", Name: "Support", VersionNumber: "2.0.0", FullName: "Owner-Support-2.0.0",
					DownloadURL: "support-download", Description: "updated support",
				}
				wantIdentities = append(wantIdentities, "Owner/Support")
				wantDownloads = append(wantDownloads, "support-download")
				wantFrom, wantTo = append(wantFrom, "Owner-Support-1.0.0"), append(wantTo, "Owner-Support-2.0.0")
			}
			releases["Owner/Earlier"] = latest
			archive := recoveryArchive(t, false, false)
			var fetched, downloaded, replacedFrom, replacedTo, written []string
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				downloaded = append(downloaded, url)
				if !slices.Contains(wantDownloads, url) {
					t.Fatalf("downloaded a blocked package: %q", url)
				}
				return &http.Response{Body: io.NopCloser(bytes.NewReader(archive))}, nil
			}}
			manager := file.NewManager(client, server)
			cleanupCause := errors.New("original cleanup interrupted")
			var staging, original string
			var replacement file.ModReplacement
			fm := &mock.Manager{
				ValidateModInstallFunc: manager.ValidateModInstall,
				ReplaceModFunc: func(url, oldFullName, newFullName string) (file.ModReplacement, error) {
					replacedFrom = append(replacedFrom, oldFullName)
					replacedTo = append(replacedTo, newFullName)
					i := slices.Index(wantFrom, oldFullName)
					if i < 0 || url != wantDownloads[i] || newFullName != wantTo[i] {
						t.Fatalf("replaced a blocked package: %q/%q/%q", url, oldFullName, newFullName)
					}
					published, err := manager.ReplaceMod(url, oldFullName, newFullName)
					if err != nil {
						return published, err
					}
					if oldFullName != "Owner-"+cleanupName+"-1.0.0" {
						return published, nil
					}
					replacement = published
					original = replacement.RecoveryPath()
					if original == "" {
						t.Fatal("replacement lost its original before recording")
					}
					staging = filepath.Dir(original)
					if cleanupFails {
						file.SetModReplacementCleanup(replacement, func(path string) error {
							if replacement.RecoveryPath() != "" || path != staging {
								t.Fatal("cleanup did not consume only its owned original")
							}
							if err := os.Remove(filepath.Join(original, "plugin.dll")); err != nil {
								t.Fatal(err)
							}
							return cleanupCause
						})
					}
					return replacement, nil
				},
			}
			r := &mock.ModsRepo{
				// Pin the sequence independently of SQLite's unspecified row ordering.
				ListModsFunc: func() ([]mod.Mod, error) { return slices.Clone(ordered), nil },
				UpsertModFunc: func(m mod.Mod) error {
					identity := m.Namespace + "/" + m.Name
					written = append(written, identity)
					if !slices.Contains(wantIdentities, identity) {
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
				release, ok := releases[identity]
				if !ok {
					t.Fatalf("fetched a blocked package: %q", identity)
				}
				return thunderstore.Package{Latest: release}, nil
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
			if staging == "" || replacement.RecoveryPath() != "" || !errors.Is(replacement.Commit(), file.ErrModReplacementFinished) {
				t.Fatal("earlier update did not consume its original recovery")
			}
			if cleanupFails {
				if err == nil || !strings.Contains(err.Error(), staging) || strings.Contains(err.Error(), "original files retained at") {
					t.Errorf("consumed cleanup artifact path lost or advertised as retryable: %v", err)
				}
				assertArchiveRecoveryFiles(t, th, staging, map[string]string{
					filepath.Join("original", "settings"): settings,
				}, nil)
			} else if _, statErr := os.Stat(staging); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("successful cleanup left its original staging: %v", statErr)
			}
			for _, calls := range []struct {
				name      string
				got, want []string
			}{
				{"fetches", fetched, wantIdentities},
				{"downloads", downloaded, wantDownloads},
				{"replacement sources", replacedFrom, wantFrom},
				{"replacement destinations", replacedTo, wantTo},
				{"record writes", written, wantIdentities},
			} {
				if !slices.Equal(calls.got, calls.want) {
					t.Errorf("%s = %v, want %v", calls.name, calls.got, calls.want)
				}
			}
			wantRecords := slices.Clone(ordered)
			for i := range wantRecords {
				m := &wantRecords[i]
				if release, updated := releases[m.Namespace+"/"+m.Name]; updated {
					delete(wantFiles, filepath.Join("BepInEx", "plugins", m.FullName(), "plugin.dll"))
					delete(wantFiles, filepath.Join("BepInEx", "plugins", m.FullName(), "settings"))
					m.Version = release.VersionNumber
					m.FilePath = filepath.Join(plugins, release.FullName)
					m.WebsiteURL, m.Description = release.WebsiteURL, release.Description
					wantFiles[filepath.Join("BepInEx", "plugins", release.FullName, "plugin.dll")] = "replacement plugin"
				}
			}
			actual, listErr := stored.ListMods()
			if listErr != nil {
				t.Fatal(listErr)
			}
			sort.Slice(actual, func(i, j int) bool { return actual[i].ID < actual[j].ID })
			if !reflect.DeepEqual(actual, wantRecords) {
				t.Errorf("records = %+v, want %+v", actual, wantRecords)
			}
			if cleanupFails {
				leftover, err := filepath.Rel(server, filepath.Join(staging, "original", "settings"))
				if err != nil {
					t.Fatal(err)
				}
				wantFiles[leftover] = settings
			}
			assertArchiveRecoveryFiles(t, th, server, wantFiles, nil)
			if entries, readErr := os.ReadDir(backups); readErr != nil || len(entries) != 0 {
				t.Errorf("unexpected shared backup artifacts: %v, %v", entries, readErr)
			}
		})
	}
}
