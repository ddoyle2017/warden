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
	"slices"
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
			"given root installation failure, when returning, then preserve original state or recovery details without completion": {
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
			"given root cleanup is configured to fail, when root metadata is rejected, then retain incurred causes without completion": {
				warning: "Root", failure: "record", failAt: "Root", oldRoot: true, published: []string{"Root"},
			},
			"given root cleanup failure, when later dependency metadata is rejected, then report an incomplete request": {
				warning: "Root", failure: "record", failAt: "Later",
				published: []string{"Root", "First", "Later"}, recorded: []string{"Root", "First"},
			},
			"given root cleanup is configured to fail, when metadata reports an error after committing, then retain recovery without compensation": {
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
			"given earlier cleanup failure, when a later bulk archive fails, then keep that package's original without whole-request completion": {
				warning: "Root", failure: "files", failAt: "Last", onlyBulk: true,
				published: []string{"Root", "First", "Later"}, recorded: []string{"Root", "First", "Later"},
			},
			"given earlier cleanup failure, when a later bulk record is rejected, then retain its original recovery and both diagnostics": {
				warning: "Root", failure: "record", failAt: "Last", onlyBulk: true,
				published: []string{"Root", "First", "Later", "Last"}, recorded: []string{"Root", "First", "Later"},
			},
			"given earlier cleanup failure, when a later bulk record has committed without acknowledgement, then retain recovery without compensation": {
				warning: "Root", failure: "commit", failAt: "Last", onlyBulk: true,
				published: []string{"Root", "First", "Later", "Last"}, recorded: []string{"Root", "First", "Later", "Last"},
			},
		} {
			if tt.onlyBulk && operation != "bulk update" {
				continue
			}
			t.Run(operation+"/"+name, func(t *testing.T) {
				replacing := operation != "add" && (tt.failAt == "Root" || tt.failAt == "Last")
				replacementPreparationFailure := replacing && tt.failure == "files"
				unconfirmedReplacement := replacing && (tt.failure == "record" || tt.failure == "commit")
				warned := tt.warning != "" && !(unconfirmedReplacement && tt.warning == tt.failAt)
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
				var artifact, retained string
				var retainedBackup file.Backup
				replacements := map[string]file.ModReplacement{}
				var installed, writes []string
				fm := &mock.Manager{
					ValidateModInstallFunc: manager.ValidateModInstall,
					ReplaceModFunc: func(url, oldFullName, newFullName string) (file.ModReplacement, error) {
						release, ok := releases[url]
						previous := prior[url]
						if !ok || newFullName != release.FullName || oldFullName != previous.FullName() {
							t.Fatalf("unexpected replacement %q/%q/%q", url, oldFullName, newFullName)
						}
						installed = append(installed, url)
						replacement, err := manager.ReplaceMod(url, oldFullName, newFullName)
						if err != nil {
							switch tt.recoveryMarker {
							case "install":
								err = errors.Join(err, &file.ModInstallCleanupError{Err: restoreCause})
							case "framework":
								err = errors.Join(err, &file.FrameworkMutationCleanupError{Err: restoreCause})
							case "removal":
								err = errors.Join(err, &file.ModRemovalCleanupError{Err: restoreCause})
							}
							return nil, err
						}
						replacements[url] = replacement
						if url == tt.warning {
							file.SetModReplacementCleanup(replacement, func(path string) error {
								artifact = path
								if replacement.RecoveryPath() != "" {
									t.Fatal("replacement cleanup retained an active original")
								}
								if err := os.Remove(filepath.Join(path, "original", "plugin.dll")); err != nil {
									t.Fatal(err)
								}
								return cleanupCause
							})
						}
						return replacement, nil
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
						t.Fatalf("installation failure deleted preserved metadata: %s/%s", namespace, name)
						return nil
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
					for _, text := range []string{"mod request completed", cleanupCause.Error(), artifact} {
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
				if _, completed := err.(*service.ModMutationCleanupError); completed != (warned && tt.failure == "") {
					t.Errorf("whole-request completion misclassified: %v", err)
				}
				var fileCompleted *file.ModInstallCleanupError
				if errors.As(err, &fileCompleted) != (warned || tt.recoveryMarker == "install") {
					t.Errorf("file completion cause lost or invented: %v", err)
				}
				if tt.failure == "" && tt.warning == "" && err != nil {
					t.Errorf("successful request failed: %v", err)
				}
				if warned {
					if !errors.Is(err, cleanupCause) || !errors.Is(err, file.ErrBackupDeleteFailed) ||
						artifact == "" || !strings.Contains(err.Error(), artifact) || strings.Contains(err.Error(), fmt.Sprintf("backup retained at %q", artifact)) {
						t.Errorf("cleanup diagnostics lost: %v", err)
					}
					leftovers := map[string]string{}
					if tt.warning != "Root" || operation == "add" {
						leftovers["z-unrelated.dll"] = "second unrelated plugin"
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
					}
					assertArchiveRecoveryFiles(t, th, artifact, leftovers, nil)
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
					if !errors.Is(err, file.ErrZipReadFailed) {
						t.Errorf("installation diagnostic lost: %v", err)
					}
					if replacementPreparationFailure {
						if errors.Is(err, file.ErrBackupRestoreFailed) || retainedBackup != nil {
							t.Errorf("preparation failure claimed an unnecessary restoration: %v", err)
						}
						if tt.recoveryMarker != "" && !errors.Is(err, restoreCause) {
							t.Errorf("nested diagnostic was lost: %v", err)
						}
					} else {
						if !errors.Is(err, file.ErrBackupRestoreFailed) || !errors.Is(err, restoreCause) ||
							retained == "" || !strings.Contains(err.Error(), retained) {
							t.Errorf("recovery diagnostics lost: %v", err)
						}
						if retainedBackup == nil || retainedBackup.Path() == nil || *retainedBackup.Path() != retained {
							t.Fatal("failed restoration lost its active snapshot")
						}
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
				if unconfirmedReplacement {
					replacement := replacements[tt.failAt]
					if replacement == nil || replacement.RecoveryPath() == "" ||
						!strings.Contains(err.Error(), replacement.RecoveryPath()) {
						t.Fatalf("unconfirmed metadata lost original files: %v", err)
					}
					assertArchiveRecoveryFiles(t, th, replacement.RecoveryPath(), map[string]string{"plugin.dll": tt.failAt + " original bytes"}, nil)
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
				if replacementPreparationFailure && tt.failAt == "Root" {
					wantRecords["Root"] = prior["Root"]
					priorRoot := prior["Root"]
					wantFiles[priorRoot.FullName()+"/plugin.dll"] = "Root original bytes"
				}
				for _, name := range []string{"First", "Last"} {
					_, recorded := wantRecords[name]
					if m, ok := prior[name]; ok && !recorded {
						wantRecords[name] = m
						if !slices.Contains(tt.published, name) {
							wantFiles[m.FullName()+"/plugin.dll"] = name + " original bytes"
						}
					}
				}
				if tt.failure == "files" && !replacementPreparationFailure {
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
				entries, readErr := os.ReadDir(backups)
				wantBackups := 0
				if warned && (operation == "add" || tt.warning != "Root") {
					wantBackups++
				}
				if tt.failure == "files" && !replacementPreparationFailure {
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
