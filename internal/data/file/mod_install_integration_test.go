package file_test

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"warden/command"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/helper"
	"warden/internal/test/mock"

	"github.com/mattn/go-sqlite3"
)

func TestModInstallRequestOutcomes(t *testing.T) {
	for _, operation := range []string{"add", "single update", "qualified update", "bulk update"} {
		for name, tt := range map[string]struct {
			warning, failure, failAt string
			recoveryMarker           string
			published, recorded      []string
			oldRoot, oldDependency   bool
			onlyBulk                 bool
		}{
			"given valid releases, when installing, then publish and record every direct dependency": {
				published: []string{"Root", "First", "Later"}, recorded: []string{"Root", "First", "Later"},
			},
			"given root cleanup failure, when installing, then record the root and finish dependencies with a warning": {
				warning: "Root", published: []string{"Root", "First", "Later"}, recorded: []string{"Root", "First", "Later"},
			},
			"given dependency cleanup failure, when installing, then record it and finish remaining dependencies": {
				warning: "First", published: []string{"Root", "First", "Later"}, recorded: []string{"Root", "First", "Later"},
			},
			"given an invalid root archive and failed recovery, when installing, then preserve recovery details without completion": {
				failure: "files", failAt: "Root",
			},
			"given a nested file completion marker, when root installation fails, then do not record completion": {
				failure: "files", failAt: "Root", recoveryMarker: "install",
			},
			"given a nested framework marker, when root installation fails, then do not infer mod completion": {
				failure: "files", failAt: "Root", recoveryMarker: "framework",
			},
			"given a nested removal marker, when root installation fails, then do not infer installation completion": {
				failure: "files", failAt: "Root", recoveryMarker: "removal",
			},
			"given root cleanup failure and an existing dependency, when dependency recovery fails, then retain its record and both diagnostics": {
				warning: "Root", failure: "files", failAt: "First", oldDependency: true,
				published: []string{"Root"}, recorded: []string{"Root"},
			},
			"given published files, when root metadata is rejected, then report the mismatch without deleting records": {
				failure: "record", failAt: "Root", oldRoot: true, published: []string{"Root"},
			},
			"given root cleanup failure, when root metadata is rejected, then retain both causes without completion": {
				warning: "Root", failure: "record", failAt: "Root", oldRoot: true, published: []string{"Root"},
			},
			"given root cleanup failure, when later dependency metadata is rejected, then report an incomplete request": {
				warning: "Root", failure: "record", failAt: "Later",
				published: []string{"Root", "First", "Later"}, recorded: []string{"Root", "First"},
			},
			"given root cleanup failure, when metadata reports an error after committing, then require verification without compensation": {
				warning: "Root", failure: "commit", failAt: "Root", published: []string{"Root"}, recorded: []string{"Root"},
			},
			"given earlier cleanup failure, when updating a later bulk mod, then finish and record every replacement": {
				warning: "Root", onlyBulk: true,
				published: []string{"Root", "First", "Later", "Last"}, recorded: []string{"Root", "First", "Later", "Last"},
			},
			"given earlier cleanup failure, when a later bulk package lookup fails, then retain warnings without request completion": {
				warning: "Root", failure: "lookup", failAt: "Last", onlyBulk: true,
				published: []string{"Root", "First", "Later"}, recorded: []string{"Root", "First", "Later"},
			},
			"given earlier cleanup failure, when a later bulk version is invalid, then retain warnings without request completion": {
				warning: "Root", failure: "version", failAt: "Last", onlyBulk: true,
				published: []string{"Root", "First", "Later"}, recorded: []string{"Root", "First", "Later"},
			},
		} {
			if tt.onlyBulk && operation != "bulk update" {
				continue
			}
			t.Run(operation+"/"+name, func(t *testing.T) {
				th := helper.NewHelper(t)
				server := th.GetValheimDirectory()
				plugins := filepath.Join(server, "BepInEx", "plugins")
				backups := t.TempDir()
				t.Setenv("TMPDIR", backups)
				original := map[string]string{"a-unrelated.dll": "first unrelated plugin", "z-unrelated.dll": "second unrelated plugin"}
				writeArchiveRecoveryFiles(t, plugins, original)
				writeArchiveRecoveryFiles(t, server, map[string]string{"world.db": "unrelated world"})
				db := th.CreateDatabase().(*sql.DB)
				repo.CreateModsTable(db)
				repo.CreateFrameworksTable(db)
				stored := repo.NewModsRepo(db)
				prior := map[string]mod.Mod{}
				seed := func(name string) {
					t.Helper()
					m := mod.Mod{Name: name, Namespace: "Owner", Version: "1.0.0", Description: "original metadata"}
					m.FilePath = filepath.Join(plugins, m.FullName())
					writeArchiveRecoveryFiles(t, m.FilePath, map[string]string{"plugin.dll": name + " original bytes"})
					if err := stored.InsertMod(m); err != nil {
						t.Fatal(err)
					}
					var err error
					prior[name], err = stored.GetModByIdentity("Owner", name)
					if err != nil {
						t.Fatal(err)
					}
				}
				if operation != "add" {
					seed("Root")
				}
				if tt.oldDependency {
					seed("First")
				}
				if tt.onlyBulk {
					seed("Last")
				}
				if tt.failure == "record" {
					for _, action := range []string{"INSERT", "UPDATE"} {
						trigger := fmt.Sprintf(`CREATE TRIGGER reject_%s BEFORE %s ON mods
							WHEN NEW.name = '%s' BEGIN SELECT RAISE(FAIL, 'metadata write interrupted'); END`, action, action, tt.failAt)
						if _, err := db.Exec(trigger); err != nil {
							t.Fatal(err)
						}
					}
				}
				releases := map[string]thunderstore.Release{}
				for _, name := range []string{"Root", "First", "Later", "Last"} {
					releases[name] = thunderstore.Release{
						Namespace: "Owner", Name: name, VersionNumber: "2.0.0", FullName: "Owner-" + name + "-2.0.0",
						DownloadURL: name, WebsiteURL: "https://example.invalid/" + name, Description: "new " + name,
					}
				}
				root := releases["Root"]
				root.Dependencies = []string{"Owner-First-1.0.0", "denikson-BepInExPack_Valheim-1.0.0", "Owner-Later-1.0.0"}
				releases["Root"] = root
				first := releases["First"]
				first.Dependencies = []string{"Owner-NotRecursive-1.0.0"}
				releases["First"] = first
				cleanupCause, restoreCause := errors.New("backup cleanup interrupted"), errors.New("recovery interrupted")
				removalCause := errors.New("removal cleanup interrupted")
				recordCause, lookupCause := errors.New("commit result unavailable"), errors.New("package lookup interrupted")
				archive := recoveryArchive(t, false, false)
				client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
					if _, ok := releases[url]; !ok {
						t.Fatalf("unexpected download %q", url)
					}
					body := archive
					if tt.failure == "files" && url == tt.failAt {
						body = []byte("bad archive")
					}
					return &http.Response{Body: io.NopCloser(bytes.NewReader(body))}, nil
				}}
				manager := file.NewManager(client, server)
				var artifact, retained, removalArtifact string
				var retainedBackup file.Backup
				var installed, writes, deleted []string
				fm := &mock.Manager{
					ValidateModInstallFunc: manager.ValidateModInstall,
					RemoveModFunc: func(fullName string) error {
						if tt.warning != "Root" || fullName != "Owner-Root-1.0.0" {
							return manager.RemoveMod(fullName)
						}
						removal, err := manager.StageModRemoval(fullName)
						if err != nil {
							t.Fatal(err)
						}
						file.SetModRemovalCleanup(removal, func(path string) error {
							removalArtifact = path
							return removalCause
						})
						return removal.Commit()
					},
					InstallModFunc: func(url, fullName string) (string, error) {
						release, ok := releases[url]
						if !ok || fullName != release.FullName {
							t.Fatalf("unexpected release %q/%q", url, fullName)
						}
						installed = append(installed, url)
						b := file.NewBackup()
						backup := &modRequestBackup{
							Backup: b,
							restore: func(destination string) error {
								if tt.failure != "files" || url != tt.failAt {
									t.Fatal("unexpected installation rollback")
								}
								retained, retainedBackup = *b.Path(), b
								var cause error = restoreCause
								switch tt.recoveryMarker {
								case "install":
									cause = &file.ModInstallCleanupError{Err: cause}
								case "framework":
									cause = &file.FrameworkMutationCleanupError{Err: cause}
								case "removal":
									cause = &file.ModRemovalCleanupError{Err: cause}
								}
								return errors.Join(file.ErrBackupRestoreFailed, cause)
							},
							remove: func() error {
								if url != tt.warning {
									return b.Remove()
								}
								return file.RemoveBackupWithCleanup(b, func(path string) error {
									artifact = path
									if b.Path() != nil {
										t.Fatal("cleanup retained an active handle")
									}
									if err := os.Remove(filepath.Join(path, "a-unrelated.dll")); err != nil {
										t.Fatal(err)
									}
									return cleanupCause
								})
							},
						}
						path, err := file.InstallModWithBackup(manager, backup, url, fullName)
						if url == tt.warning && (b.Path() != nil || !errors.Is(b.Restore(plugins), file.ErrBackupMissing)) {
							t.Fatal("partially deleted backup remained retryable")
						}
						return path, err
					},
				}
				r := &mock.ModsRepo{
					GetModFunc: stored.GetMod, GetModByIdentityFunc: stored.GetModByIdentity,
					ListModsFunc: func() ([]mod.Mod, error) {
						mods, err := stored.ListMods()
						sort.Slice(mods, func(i, j int) bool { return mods[i].ID < mods[j].ID })
						return mods, err
					},
					UpsertModFunc: func(m mod.Mod) error {
						writes = append(writes, m.Name)
						if err := stored.UpsertMod(m); err != nil {
							return err
						}
						if tt.failure == "commit" && m.Name == tt.failAt {
							return fmt.Errorf("commit transaction: %w", recordCause)
						}
						return nil
					},
					DeleteModFunc: func(name, namespace string) error {
						if name != "Root" || namespace != "Owner" || operation == "add" || tt.failure != "files" || tt.failAt != "Root" {
							t.Fatalf("deletion exceeded obsolete-update-record ownership: %s/%s", namespace, name)
						}
						deleted = append(deleted, name)
						return stored.DeleteMod(name, namespace)
					},
				}
				var fetched []string
				ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					release, ok := releases[name]
					if namespace != "Owner" || !ok {
						t.Fatalf("unexpected dependency policy or package identity: %s/%s", namespace, name)
					}
					fetched = append(fetched, name)
					if tt.failure == "lookup" && name == tt.failAt {
						return thunderstore.Package{}, lookupCause
					}
					if tt.failure == "version" && name == tt.failAt {
						release.VersionNumber = "invalid"
					}
					return thunderstore.Package{Latest: release}, nil
				}}
				svc := service.NewModService(r, fm, ts, strings.NewReader("Y\n"))
				var err error
				if operation == "qualified update" && tt.warning == "Root" && tt.failure == "" {
					t.Setenv("HOME", t.TempDir())
					observed := &observedModUpdate{Mod: svc}
					cmd := command.NewUpdateCommand(nil, observed)
					cmd.SetArgs([]string{"--namespace", "Owner", "--mod", "Root"})
					output, createErr := os.CreateTemp(t.TempDir(), "stdout")
					if createErr != nil {
						t.Fatal(createErr)
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
					for _, text := range []string{"mod request completed", cleanupCause.Error(), removalCause.Error(), artifact, removalArtifact} {
						if text == "" || strings.Count(string(data), text) != 1 {
							t.Errorf("cross-layer diagnostic must appear once: %q in %q", text, data)
						}
					}
					if strings.Contains(string(data), "successfully") {
						t.Errorf("cleanup warnings reported as ordinary success: %q", data)
					}
				} else {
					err = runModInstallRequest(svc, operation)
				}
				if _, completed := err.(*service.ModMutationCleanupError); completed != (tt.warning != "" && tt.failure == "") {
					t.Errorf("whole-request completion misclassified: %v", err)
				}
				var fileCompleted *file.ModInstallCleanupError
				if errors.As(err, &fileCompleted) != (tt.warning != "" || tt.recoveryMarker == "install") {
					t.Errorf("file completion cause lost or invented: %v", err)
				}
				if tt.failure == "" && tt.warning == "" && err != nil {
					t.Errorf("successful request failed: %v", err)
				}
				if tt.warning != "" {
					if !errors.Is(err, cleanupCause) || !errors.Is(err, file.ErrBackupDeleteFailed) ||
						artifact == "" || !strings.Contains(err.Error(), artifact) || strings.Contains(err.Error(), fmt.Sprintf("backup retained at %q", artifact)) {
						t.Errorf("cleanup diagnostics lost: %v", err)
					}
					leftovers := map[string]string{"z-unrelated.dll": "second unrelated plugin"}
					if tt.warning == "First" {
						leftovers[root.FullName+"/plugin.dll"] = "replacement plugin"
					}
					if tt.oldDependency {
						dependency := prior["First"]
						leftovers[dependency.FullName()+"/plugin.dll"] = "First original bytes"
					}
					if tt.onlyBulk {
						last := prior["Last"]
						leftovers[last.FullName()+"/plugin.dll"] = "Last original bytes"
					}
					assertArchiveRecoveryFiles(t, th, artifact, leftovers, nil)
				}
				if tt.warning == "Root" && operation != "add" {
					if !errors.Is(err, removalCause) || removalArtifact == "" || !strings.Contains(err.Error(), removalArtifact) {
						t.Errorf("earlier removal diagnostic lost: %v", err)
					}
					assertArchiveRecoveryFiles(t, th, filepath.Join(removalArtifact, "contents"), map[string]string{"plugin.dll": "Root original bytes"}, nil)
				}
				if tt.failure != "" {
					sentinel := service.ErrModInstallFailed
					if operation != "add" {
						sentinel = service.ErrUnableToUpdateMod
					}
					if tt.failAt == "First" || tt.failAt == "Later" {
						sentinel = service.ErrAddDependenciesFailed
					} else if tt.failure == "lookup" {
						sentinel = service.ErrModNotFound
					}
					if !errors.Is(err, sentinel) {
						t.Errorf("request failure sentinel lost: %v", err)
					}
				}
				switch tt.failure {
				case "files":
					if !errors.Is(err, file.ErrZipReadFailed) || !errors.Is(err, file.ErrBackupRestoreFailed) ||
						!errors.Is(err, restoreCause) || retained == "" || !strings.Contains(err.Error(), retained) {
						t.Errorf("installation/recovery diagnostics lost: %v", err)
					}
					if retainedBackup == nil || retainedBackup.Path() == nil || *retainedBackup.Path() != retained {
						t.Fatal("failed restoration lost its active snapshot")
					}
				case "record":
					var cause sqlite3.Error
					if !errors.As(err, &cause) || cause.ExtendedCode != sqlite3.ErrConstraintTrigger {
						t.Errorf("SQLite record rejection cause lost: %v", err)
					}
				case "commit":
					if !errors.Is(err, recordCause) {
						t.Errorf("unknown commit outcome lost: %v", err)
					}
				case "lookup":
					if !errors.Is(err, lookupCause) {
						t.Errorf("later lookup cause lost: %v", err)
					}
				case "version":
					if !errors.Is(err, service.ErrInvalidReleaseVersion) {
						t.Errorf("later version validation lost: %v", err)
					}
				}
				if tt.failure == "record" || tt.failure == "commit" {
					if err == nil || !strings.Contains(err.Error(), "files installed at") ||
						!strings.Contains(err.Error(), "verify database state") ||
						!strings.Contains(err.Error(), filepath.Join(plugins, releases[tt.failAt].FullName)) {
						t.Errorf("metadata uncertainty or installed path lost: %v", err)
					}
				}
				wantFiles := map[string]string{"a-unrelated.dll": "first unrelated plugin", "z-unrelated.dll": "second unrelated plugin"}
				wantRecords := map[string]mod.Mod{}
				for _, name := range tt.published {
					wantFiles[releases[name].FullName+"/plugin.dll"] = "replacement plugin"
				}
				for _, name := range tt.recorded {
					release := releases[name]
					wantRecords[name] = mod.Mod{
						ID: prior[name].ID, Namespace: "Owner", Name: name, Version: "2.0.0",
						FilePath: filepath.Join(plugins, release.FullName), WebsiteURL: release.WebsiteURL, Description: release.Description,
					}
				}
				if tt.oldRoot && operation != "add" {
					wantRecords["Root"] = prior["Root"]
				}
				for _, name := range []string{"First", "Last"} {
					_, recorded := wantRecords[name]
					if m, ok := prior[name]; ok && !recorded {
						wantRecords[name] = m
						wantFiles[m.FullName()+"/plugin.dll"] = name + " original bytes"
					}
				}
				if tt.failure == "files" {
					assertArchiveRecoveryFiles(t, th, retained, wantFiles, nil)
					wantFiles[releases[tt.failAt].FullName+".zip"] = "bad archive"
				}
				assertArchiveRecoveryFiles(t, th, plugins, wantFiles, nil)
				if data, readErr := os.ReadFile(filepath.Join(server, "world.db")); readErr != nil || string(data) != "unrelated world" {
					t.Errorf("unrelated server data changed: %q, %v", data, readErr)
				}
				actual, listErr := stored.ListMods()
				if listErr != nil || len(actual) != len(wantRecords) {
					t.Fatalf("records = %+v, %v; want %+v", actual, listErr, wantRecords)
				}
				for _, got := range actual {
					want, ok := wantRecords[got.Name]
					if want.ID == 0 {
						want.ID = got.ID
					}
					if !ok || got.ID <= 0 || !reflect.DeepEqual(got, want) {
						t.Errorf("record = %+v, want %+v", got, want)
					}
				}
				wantInstalled := append([]string(nil), tt.published...)
				if tt.failure == "files" {
					wantInstalled = append(wantInstalled, tt.failAt)
				}
				wantFetched := append([]string(nil), wantInstalled...)
				if tt.onlyBulk && (tt.failure == "lookup" || tt.failure == "version") {
					wantFetched = append(wantFetched, "Last")
				}
				if !reflect.DeepEqual(installed, wantInstalled) || !reflect.DeepEqual(writes, tt.published) ||
					!reflect.DeepEqual(fetched, wantFetched) {
					t.Errorf("request stopped or continued at wrong phase: fetched=%v installed=%v written=%v; want %v/%v/%v",
						fetched, installed, writes, wantFetched, wantInstalled, tt.published)
				}
				wantDeletes := 0
				if operation != "add" && tt.failure == "files" && tt.failAt == "Root" {
					wantDeletes = 1
				}
				if len(deleted) != wantDeletes {
					t.Errorf("obsolete record deletes = %v, want %d", deleted, wantDeletes)
				}
				entries, readErr := os.ReadDir(backups)
				wantBackups := 0
				if tt.warning != "" {
					wantBackups++
				}
				if tt.failure == "files" {
					wantBackups++
				}
				if readErr != nil || len(entries) != wantBackups {
					t.Errorf("unexpected backup artifacts: %v, %v; want %d", entries, readErr, wantBackups)
				}
			})
		}
	}
}

func runModInstallRequest(svc service.Mod, operation string) error {
	switch operation {
	case "add":
		return svc.AddMod("Owner", "Root")
	case "single update":
		return svc.UpdateMod("Root")
	case "qualified update":
		return svc.UpdateModByIdentity("Owner", "Root")
	case "bulk update":
		return svc.UpdateAllMods()
	default:
		panic("unconfigured mod request")
	}
}

type modRequestBackup struct {
	file.Backup
	restore func(string) error
	remove  func() error
}

func (b *modRequestBackup) Restore(destination string) error { return b.restore(destination) }
func (b *modRequestBackup) Remove() error                    { return b.remove() }

type observedModUpdate struct {
	service.Mod
	result error
}

func (s *observedModUpdate) UpdateModByIdentity(namespace, name string) error {
	s.result = s.Mod.UpdateModByIdentity(namespace, name)
	return s.result
}
