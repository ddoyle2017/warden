package command_test

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"warden/command"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/helper"
	"warden/internal/test/mock"

	"github.com/spf13/cobra"
)

func TestAddCommandBootstrapConsent(t *testing.T) {
	for name, tt := range map[string]struct {
		answer              string
		installed, wantWork bool
	}{
		"given a missing framework, when declining bootstrap, then do not fetch or install the mod": {
			answer: "n\n",
		},
		"given a missing framework, when confirming bootstrap, then install and record both packages": {
			answer: "Y\n", wantWork: true,
		},
		"given a recorded framework, when adding a mod, then reuse it without confirmation": {
			installed: true, wantWork: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			backups := t.TempDir()
			t.Setenv("TMPDIR", backups)
			th := helper.NewHelper(t)
			server := th.GetValheimDirectory()
			db := th.CreateDatabase().(*sql.DB)
			db.SetMaxOpenConns(1)
			repo.CreateFrameworksTable(db)
			repo.CreateModsTable(db)
			mr, fr := repo.NewModsRepo(db), repo.NewFrameworksRepo(db)
			root := thunderstore.Release{
				Namespace: "Owner", Name: "Root", VersionNumber: "1.0.0", FullName: "Owner-Root-1.0.0",
				DownloadURL: "https://example.invalid/root.zip", Dependencies: []string{},
			}
			loader := thunderstore.Release{
				Namespace: framework.BepInExNamespace, Name: framework.BepInEx, VersionNumber: "1.0.0",
				FullName: "denikson-BepInExPack_Valheim-1.0.0", DownloadURL: "https://example.invalid/framework.zip",
			}
			writeCommandFile(t, filepath.Join(server, "world.db"), "unrelated world")
			if tt.installed {
				if err := fr.InsertFramework(framework.Framework{
					Namespace: loader.Namespace, Name: loader.Name, Version: loader.VersionNumber,
				}); err != nil {
					t.Fatal(err)
				}
				writeCommandFile(t, filepath.Join(server, "BepInEx", "core", "runtime"), "existing framework")
			}
			before := commandTree(t, server)
			beforeChanges := commandDatabaseChanges(t, db)
			packages, downloads := map[string]int{}, map[string]int{}
			ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
				packages[name]++
				switch {
				case namespace == root.Namespace && name == root.Name:
					return thunderstore.Package{Latest: root}, nil
				case namespace == loader.Namespace && name == loader.Name:
					return thunderstore.Package{Latest: loader}, nil
				default:
					t.Fatalf("unexpected package lookup %s/%s", namespace, name)
					return thunderstore.Package{}, nil
				}
			}}
			archives := map[string][]byte{
				root.DownloadURL: commandArchive(t, map[string]string{"plugin.dll": "requested mod"}),
				loader.DownloadURL: commandArchive(t, map[string]string{
					"BepInExPack_Valheim/BepInEx/core/runtime": "installed framework",
				}),
			}
			manager := file.NewManager(&mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				body, ok := archives[url]
				if !ok {
					t.Fatalf("unexpected download %q", url)
				}
				downloads[url]++
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}, nil
			}}, server)
			ms := &observedOutcomeAdd{Mod: service.NewModService(mr, manager, ts, strings.NewReader(""))}
			in := &countedCommandReader{Reader: strings.NewReader(tt.answer)}
			frameworks := service.NewFrameworkService(fr, manager, ts, in)
			cmd := command.NewAddCommand(frameworks, ms)
			cmd.SetArgs([]string{"--namespace", root.Namespace, "--mod", root.Name})
			output := runOutcomeCommand(t, cmd)

			wantMod, wantFramework, wantReads := 0, 0, 1
			if tt.wantWork {
				wantMod = 1
				if !tt.installed {
					wantFramework = 1
				}
			}
			if tt.installed {
				wantReads = 0
			}
			if ms.calls != wantMod || packages[root.Name] != wantMod || downloads[root.DownloadURL] != wantMod ||
				packages[loader.Name] != wantFramework || downloads[loader.DownloadURL] != wantFramework || in.reads != wantReads {
				t.Errorf("unexpected add work: calls=%d, fetches=%v, downloads=%v, reads=%d", ms.calls, packages, downloads, in.reads)
			}
			if got := commandDatabaseChanges(t, db) - beforeChanges; got != wantMod+wantFramework {
				t.Errorf("database changes = %d, want %d", got, wantMod+wantFramework)
			}
			records, err := mr.ListMods()
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != wantMod {
				t.Errorf("installed records = %+v; want %d", records, wantMod)
			}
			if tt.wantWork {
				if len(records) != 1 {
					t.Fatal("completed add did not produce exactly one mod record")
				}
				if ms.result != nil {
					t.Errorf("completed add returned %v", ms.result)
				}
				path := filepath.Join(server, "BepInEx", "plugins", root.FullName)
				want := mod.Mod{
					ID: records[0].ID, Namespace: root.Namespace, Name: root.Name,
					Version: root.VersionNumber, FilePath: path,
				}
				if records[0].ID <= 0 || !reflect.DeepEqual(records[0], want) {
					t.Errorf("installed record = %+v, want %+v", records[0], want)
				}
				if data, err := os.ReadFile(filepath.Join(path, "plugin.dll")); err != nil || string(data) != "requested mod" {
					t.Errorf("mod payload = %q, %v", data, err)
				}
				record, err := fr.GetFramework(framework.BepInEx)
				if err != nil || record.Namespace != loader.Namespace || record.Version != loader.VersionNumber {
					t.Errorf("framework was not ready: %+v, %v", record, err)
				}
				wantRuntime := "installed framework"
				if tt.installed {
					wantRuntime = "existing framework"
				}
				if data, err := os.ReadFile(filepath.Join(server, "BepInEx", "core", "runtime")); err != nil || string(data) != wantRuntime {
					t.Errorf("framework payload = %q, %v; want %q", data, err, wantRuntime)
				}
				if strings.Count(output, "successfully installed mod") != 1 {
					t.Errorf("completed add omitted or duplicated success: %q", output)
				}
			} else {
				if after := commandTree(t, server); !reflect.DeepEqual(after, before) {
					t.Errorf("declined bootstrap changed files: %v, want %v", after, before)
				}
				if _, err := fr.GetFramework(framework.BepInEx); !errors.Is(err, repo.ErrFrameworkFetchNoResults) {
					t.Errorf("declined bootstrap recorded a framework: %v", err)
				}
				if strings.Count(output, "aborting") != 1 || strings.Contains(output, "successfully") {
					t.Errorf("declined bootstrap reported success: %q", output)
				}
				assertNoFailureDiagnostic(t, output)
			}
			if data, err := os.ReadFile(filepath.Join(server, "world.db")); err != nil || string(data) != "unrelated world" {
				t.Errorf("unrelated world changed: %q, %v", data, err)
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("unexpected backup artifacts: %v, %v", entries, err)
			}
		})
	}
}

func TestCommandsWithoutMutation(t *testing.T) {
	for _, operation := range []string{"update mod", "update qualified", "update all", "update framework", "remove mod", "remove all", "remove framework"} {
		outcomes := []string{"declined"}
		if strings.HasPrefix(operation, "update") {
			outcomes = append(outcomes, "unchanged")
		}
		if operation == "update all" {
			outcomes = append(outcomes, "empty")
		}
		for _, outcome := range outcomes {
			t.Run("given "+outcome+" "+operation+", when returning, then preserve all state without success text", func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				backups := t.TempDir()
				t.Setenv("TMPDIR", backups)
				th := helper.NewHelper(t)
				server := th.GetValheimDirectory()
				db := th.CreateDatabase().(*sql.DB)
				db.SetMaxOpenConns(1)
				repo.CreateFrameworksTable(db)
				repo.CreateModsTable(db)
				mr, fr := repo.NewModsRepo(db), repo.NewFrameworksRepo(db)
				current := mod.Mod{Namespace: "Owner", Name: "Root", Version: "1.0.0"}
				current.FilePath = filepath.Join(server, "BepInEx", "plugins", current.FullName())
				if outcome != "empty" {
					if err := mr.InsertMod(current); err != nil {
						t.Fatal(err)
					}
					writeCommandFile(t, filepath.Join(current.FilePath, "plugin.dll"), "original mod")
				}
				if err := fr.InsertFramework(framework.Framework{
					Namespace: framework.BepInExNamespace, Name: framework.BepInEx, Version: "1.0.0",
				}); err != nil {
					t.Fatal(err)
				}
				writeCommandFile(t, filepath.Join(server, "BepInEx", "core", "runtime"), "original framework")
				writeCommandFile(t, filepath.Join(server, "world.db"), "unrelated world")
				beforeFiles := commandTree(t, server)
				beforeMods, err := mr.ListMods()
				if err != nil {
					t.Fatal(err)
				}
				beforeFramework, err := fr.GetFramework(framework.BepInEx)
				if err != nil {
					t.Fatal(err)
				}
				beforeChanges := commandDatabaseChanges(t, db)
				fetches, downloads := 0, 0
				ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					fetches++
					wantNamespace, wantName := current.Namespace, current.Name
					if operation == "update framework" {
						wantNamespace, wantName = framework.BepInExNamespace, framework.BepInEx
					}
					if namespace != wantNamespace || name != wantName {
						t.Fatalf("unexpected package %s/%s", namespace, name)
					}
					version := "2.0.0"
					if outcome == "unchanged" {
						version = "1.0.0"
					}
					return thunderstore.Package{Latest: thunderstore.Release{
						Namespace: namespace, Name: name, VersionNumber: version,
					}}, nil
				}}
				manager := file.NewManager(&mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
					downloads++
					t.Fatalf("unconfirmed or unchanged operation downloaded %q", url)
					return nil, errors.New("unexpected download")
				}}, server)
				answer := "n\n"
				if outcome == "unchanged" || outcome == "empty" {
					answer = "Y\n"
				} else if operation == "remove all" || operation == "remove framework" {
					answer = "no\n"
				}
				in := &countedCommandReader{Reader: strings.NewReader(answer)}
				ms := service.NewModService(mr, manager, ts, in)
				frameworks := service.NewFrameworkService(fr, manager, ts, in)
				cmd := command.NewUpdateCommand(frameworks, ms)
				args := []string{"--mod", current.Name}
				switch operation {
				case "update qualified":
					args = []string{"--namespace", current.Namespace, "--mod", current.Name}
				case "update all":
					args = []string{"all"}
				case "update framework":
					args = []string{"bepinex"}
				case "remove mod":
					cmd = command.NewRemoveCommand(frameworks, ms)
					args = []string{"--namespace", current.Namespace, "--mod", current.Name}
				case "remove all":
					cmd = command.NewRemoveCommand(frameworks, ms)
					args = []string{"all"}
				case "remove framework":
					cmd = command.NewRemoveCommand(frameworks, ms)
					args = []string{"bepinex"}
				}
				cmd.SetArgs(args)
				output := runOutcomeCommand(t, cmd)
				wantFetches, wantReads := 0, 1
				if strings.HasPrefix(operation, "update") && (operation != "update all" || outcome == "unchanged") {
					wantFetches = 1
				}
				if outcome == "unchanged" && operation != "update all" {
					wantReads = 0
				}
				if fetches != wantFetches || downloads != 0 || in.reads != wantReads {
					t.Errorf("unexpected fetches/downloads/reads = %d/%d/%d, want %d/0/%d", fetches, downloads, in.reads, wantFetches, wantReads)
				}
				if after := commandTree(t, server); !reflect.DeepEqual(after, beforeFiles) {
					t.Errorf("no-work command changed files: %v, want %v", after, beforeFiles)
				}
				afterMods, err := mr.ListMods()
				if err != nil || !reflect.DeepEqual(afterMods, beforeMods) {
					t.Errorf("no-work command changed mod records: %+v, %v", afterMods, err)
				}
				afterFramework, err := fr.GetFramework(framework.BepInEx)
				if err != nil || afterFramework != beforeFramework {
					t.Errorf("no-work command changed framework record: %+v, %v", afterFramework, err)
				}
				if commandDatabaseChanges(t, db) != beforeChanges {
					t.Error("no-work command issued a database mutation")
				}
				if outcome == "declined" && strings.Count(output, "aborting") != 1 {
					t.Errorf("cancellation was not reported once: %q", output)
				}
				if outcome == "unchanged" && !strings.Contains(output, "already installed") && !strings.Contains(output, "up-to-date") {
					t.Errorf("unchanged outcome was not reported: %q", output)
				}
				if outcome == "empty" && !strings.Contains(output, "no mods need updating") {
					t.Errorf("empty bulk update omitted its no-change outcome: %q", output)
				}
				if strings.Contains(output, "successfully") || strings.Contains(output, "warning:") {
					t.Errorf("no-work command claimed a completed mutation: %q", output)
				}
				assertNoFailureDiagnostic(t, output)
				if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
					t.Errorf("no-work command left backup artifacts: %v, %v", entries, err)
				}
			})
		}
	}
}

type observedOutcomeAdd struct {
	service.Mod
	calls  int
	result error
}

func (s *observedOutcomeAdd) AddMod(namespace, name string) error {
	s.calls++
	s.result = s.Mod.AddMod(namespace, name)
	return s.result
}

type countedCommandReader struct {
	io.Reader
	reads int
}

func (r *countedCommandReader) Read(p []byte) (int, error) {
	r.reads++
	return r.Reader.Read(p)
}

func commandDatabaseChanges(t *testing.T, db *sql.DB) int {
	t.Helper()
	var changes int
	if err := db.QueryRow(`SELECT total_changes()`).Scan(&changes); err != nil {
		t.Fatal(err)
	}
	return changes
}

func writeCommandFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func commandTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			tree[relative+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		tree[relative] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func commandArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var body bytes.Buffer
	archive := zip.NewWriter(&body)
	for name, contents := range entries {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func assertNoFailureDiagnostic(t *testing.T, output string) {
	t.Helper()
	for _, text := range []string{"unable to", "error:", service.ErrCancelled.Error(), service.ErrNoChanges.Error()} {
		if strings.Contains(strings.ToLower(output), text) {
			t.Errorf("benign outcome contains failure diagnostic %q: %q", text, output)
		}
	}
}

func runOutcomeCommand(t *testing.T, cmd *cobra.Command) string {
	t.Helper()
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	defer func() {
		os.Stdout = stdout
		if err := output.Close(); err != nil {
			t.Error(err)
		}
	}()
	os.Stdout = output
	cmd.SetOut(output)
	cmd.SetErr(output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdout
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
