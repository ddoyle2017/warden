package command_test

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
)

func TestAddCommandReadinessOutcomes(t *testing.T) {
	for name, tt := range map[string]struct {
		existing, missingDependency, packageFailure bool
		metadataFailure                             string
		wantFramework, wantRoot, wantDependency     bool
		wantAdds, wantRootFetches, wantDepFetches   int
		wantModResult                               error
	}{
		"given completed bootstrap cleanup, when adding a mod, then continue and report each outcome": {
			wantFramework: true, wantRoot: true, wantAdds: 1, wantRootFetches: 1,
		},
		"given completed bootstrap cleanup, when the mod lookup fails, then do not report mod completion": {
			packageFailure: true, wantFramework: true, wantAdds: 1, wantRootFetches: 1, wantModResult: service.ErrModNotFound,
		},
		"given a recorded framework and an incomplete root, when adding, then repair only the missing dependency": {
			existing: true, missingDependency: true, wantFramework: true, wantRoot: true, wantDependency: true, wantAdds: 1, wantDepFetches: 1,
		},
		"given a recorded framework and a complete root, when adding, then report the mod as unchanged": {
			existing: true, wantFramework: true, wantRoot: true, wantAdds: 1, wantModResult: service.ErrModAlreadyInstalled,
		},
		"given file cleanup and rejected framework metadata, when adding, then stop before any mod work": {
			metadataFailure: "rejected",
		},
		"given file cleanup and uncertain framework commit, when adding, then stop despite the persisted record": {
			metadataFailure: "uncertain", wantFramework: true,
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
			dependency := thunderstore.Release{
				Namespace: "Required", Name: "Dependency", VersionNumber: "2.0.0", FullName: "Required-Dependency-2.0.0",
				DownloadURL: "https://example.invalid/dependency.zip",
			}
			loader := thunderstore.Release{
				Namespace: framework.BepInExNamespace, Name: framework.BepInEx, VersionNumber: "1.0.0",
				FullName: "denikson-BepInExPack_Valheim-1.0.0", DownloadURL: "https://example.invalid/framework.zip",
			}
			if tt.missingDependency {
				root.Dependencies = []string{"Required-Dependency-1.0.0"}
			}
			rootPath := filepath.Join(server, "BepInEx", "plugins", root.FullName)
			var original mod.Mod
			if tt.existing {
				if err := fr.InsertFramework(framework.Framework{
					Namespace: loader.Namespace, Name: loader.Name, Version: loader.VersionNumber,
				}); err != nil {
					t.Fatal(err)
				}
				writeCommandFile(t, filepath.Join(server, "BepInEx", "core", "runtime"), "framework bytes")
				if err := mr.InsertMod(mod.Mod{
					Namespace: root.Namespace, Name: root.Name, Version: root.VersionNumber, FilePath: rootPath,
					Description: "original root metadata",
				}); err != nil {
					t.Fatal(err)
				}
				var err error
				original, err = mr.GetModByIdentity(root.Namespace, root.Name)
				if err != nil {
					t.Fatal(err)
				}
				writeCommandFile(t, filepath.Join(rootPath, "plugin.dll"), "original root bytes")
			}
			if tt.metadataFailure == "rejected" {
				if _, err := db.Exec(`CREATE TRIGGER reject_framework BEFORE INSERT ON frameworks
					BEGIN SELECT RAISE(FAIL, 'framework metadata rejected'); END`); err != nil {
					t.Fatal(err)
				}
			}
			artifact := filepath.Join(t.TempDir(), "bootstrap-cleanup")
			cleanupCause := errors.New("bootstrap cleanup interrupted")
			commitCause := errors.New("framework commit acknowledgement lost")
			modCause := errors.New("requested package unavailable")
			cleanup := &file.FrameworkMutationCleanupError{Err: fmt.Errorf("%w at %q: %w", file.ErrBackupDeleteFailed, artifact, cleanupCause)}
			packages, downloads := map[string]int{}, map[string]int{}
			exactFetches := 0
			ts := &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					packages[name]++
					switch {
					case namespace == loader.Namespace && name == loader.Name:
						return thunderstore.Package{Latest: loader}, nil
					case namespace == root.Namespace && name == root.Name:
						if tt.packageFailure {
							return thunderstore.Package{}, modCause
						}
						return thunderstore.Package{Latest: root}, nil
					case namespace == dependency.Namespace && name == dependency.Name:
						return thunderstore.Package{Latest: dependency}, nil
					default:
						t.Fatalf("unexpected package %s/%s", namespace, name)
						return thunderstore.Package{}, nil
					}
				},
				GetReleaseFunc: func(namespace, name, version string) (thunderstore.Release, error) {
					exactFetches++
					if !tt.existing || namespace != root.Namespace || name != root.Name || version != "1.0.0" {
						t.Fatalf("unexpected exact release %s/%s/%s", namespace, name, version)
					}
					return root, nil
				},
			}
			archives := map[string][]byte{
				root.DownloadURL:       commandArchive(t, map[string]string{"plugin.dll": "new root bytes"}),
				dependency.DownloadURL: commandArchive(t, map[string]string{"plugin.dll": "dependency bytes"}),
				loader.DownloadURL: commandArchive(t, map[string]string{
					"BepInExPack_Valheim/BepInEx/core/runtime": "framework bytes",
				}),
			}
			manager := file.NewManager(&mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				body, ok := archives[url]
				if !ok {
					t.Fatalf("unexpected download %q", url)
				}
				downloads[url]++
				return &http.Response{Body: io.NopCloser(bytes.NewReader(body))}, nil
			}}, server)
			fm := &mock.Manager{
				InstallModFunc: manager.InstallMod,
				InstallBepInExFunc: func(url, fullName string) (string, error) {
					path, err := manager.InstallBepInEx(url, fullName)
					if err != nil {
						return path, err
					}
					writeCommandFile(t, artifact, "partial cleanup artifact")
					return path, cleanup
				},
			}
			frameworkWrites := 0
			records := &mock.FrameworksRepo{
				GetFrameworkFunc: fr.GetFramework,
				InsertFrameworkFunc: func(f framework.Framework) error {
					frameworkWrites++
					if err := fr.InsertFramework(f); err != nil {
						return err
					}
					if tt.metadataFailure == "uncertain" {
						return errors.Join(repo.ErrFrameworkInsertFailed, commitCause)
					}
					return nil
				},
			}
			frameworks := &observedOutcomeFramework{Framework: service.NewFrameworkService(records, fm, ts, strings.NewReader("Y\n"))}
			ms := &observedOutcomeAdd{Mod: service.NewModService(mr, fm, ts, strings.NewReader(""))}
			cmd := command.NewAddCommand(frameworks, ms)
			cmd.SetArgs([]string{"--namespace", root.Namespace, "--mod", root.Name})
			output := runOutcomeCommand(t, cmd)

			wantBootstrap := 1
			if tt.existing {
				wantBootstrap = 0
			}
			if frameworkWrites != wantBootstrap || packages[loader.Name] != wantBootstrap || downloads[loader.DownloadURL] != wantBootstrap ||
				ms.calls != tt.wantAdds || packages[root.Name] != tt.wantRootFetches || packages[dependency.Name] != tt.wantDepFetches {
				t.Errorf("unexpected work: framework writes=%d, add calls=%d, fetches=%v, downloads=%v", frameworkWrites, ms.calls, packages, downloads)
			}
			wantExact, wantRootDownloads := 0, 0
			if tt.existing {
				wantExact = 1
			} else if tt.wantRoot {
				wantRootDownloads = 1
			}
			if exactFetches != wantExact || downloads[root.DownloadURL] != wantRootDownloads || downloads[dependency.DownloadURL] != tt.wantDepFetches {
				t.Errorf("unexpected release work: exact fetches=%d, downloads=%v", exactFetches, downloads)
			}
			if !errors.Is(ms.result, tt.wantModResult) {
				t.Errorf("mod outcome = %v, want %v", ms.result, tt.wantModResult)
			}
			if tt.packageFailure && !errors.Is(ms.result, modCause) {
				t.Errorf("requested mod failure was lost: %v", ms.result)
			}
			if tt.existing {
				if frameworks.result != service.ErrNoChanges {
					t.Errorf("recorded framework readiness = %v, want no changes", frameworks.result)
				}
			} else {
				_, completed := frameworks.result.(*service.FrameworkMutationCleanupError)
				if completed != (tt.metadataFailure == "") || !errors.Is(frameworks.result, cleanupCause) {
					t.Errorf("bootstrap outcome misclassified: %v", frameworks.result)
				}
				var leaf *file.FrameworkMutationCleanupError
				if !errors.As(frameworks.result, &leaf) || !errors.Is(frameworks.result, cleanup) {
					t.Errorf("bootstrap lost file cleanup compatibility: %v", frameworks.result)
				}
			}
			wantWarnings := wantBootstrap
			if tt.metadataFailure != "" {
				wantWarnings = 0
				if !errors.Is(frameworks.result, service.ErrUnableToInstallFramework) ||
					!errors.Is(frameworks.result, repo.ErrFrameworkInsertFailed) ||
					!strings.Contains(output, "verify database state before retrying") {
					t.Errorf("metadata uncertainty was hidden: %v, %q", frameworks.result, output)
				}
			}
			if tt.metadataFailure == "uncertain" && !errors.Is(frameworks.result, commitCause) {
				t.Errorf("unknown commit lost its cause: %v", frameworks.result)
			}
			for _, text := range []string{artifact, cleanupCause.Error()} {
				if strings.Count(output, text) != wantBootstrap {
					t.Errorf("bootstrap diagnostic %q must appear %d times: %q", text, wantBootstrap, output)
				}
			}
			wantSuccess := tt.wantAdds == 1 && tt.wantModResult == nil
			if strings.Count(output, "warning:") != wantWarnings ||
				strings.Contains(output, "successfully installed mod") != wantSuccess ||
				strings.Contains(output, "mod request completed") {
				t.Errorf("bootstrap was confused with requested-mod completion: %q", output)
			}
			frameworkRecord, err := fr.GetFramework(framework.BepInEx)
			if tt.wantFramework {
				if err != nil || frameworkRecord.Version != loader.VersionNumber || frameworkRecord.Namespace != loader.Namespace {
					t.Errorf("framework record = %+v, %v", frameworkRecord, err)
				}
			} else if !errors.Is(err, repo.ErrFrameworkFetchNoResults) {
				t.Errorf("rejected framework metadata was recorded: %+v, %v", frameworkRecord, err)
			}
			if data, err := os.ReadFile(filepath.Join(server, "BepInEx", "core", "runtime")); err != nil || string(data) != "framework bytes" {
				t.Errorf("framework file state = %q, %v", data, err)
			}
			rootRecord, err := mr.GetModByIdentity(root.Namespace, root.Name)
			if tt.wantRoot {
				wantBytes := "new root bytes"
				if tt.existing {
					wantBytes = "original root bytes"
					if !reflect.DeepEqual(rootRecord, original) {
						t.Errorf("retry changed the root record: %+v, want %+v", rootRecord, original)
					}
				}
				if err != nil || rootRecord.Version != "1.0.0" || rootRecord.FilePath != rootPath {
					t.Errorf("root record = %+v, %v", rootRecord, err)
				}
				if data, err := os.ReadFile(filepath.Join(rootPath, "plugin.dll")); err != nil || string(data) != wantBytes {
					t.Errorf("root bytes = %q, %v; want %q", data, err, wantBytes)
				}
			} else if !errors.Is(err, repo.ErrModFetchNoResults) {
				t.Errorf("uncompleted root was recorded: %+v, %v", rootRecord, err)
			} else if _, err := os.Lstat(rootPath); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("blocked add created root files: %v", err)
			}
			depRecord, err := mr.GetModByIdentity(dependency.Namespace, dependency.Name)
			if tt.wantDependency {
				if err != nil || depRecord.Version != "2.0.0" || depRecord.FilePath == "" {
					t.Fatalf("missing dependency was not repaired: %+v, %v", depRecord, err)
				}
				if data, err := os.ReadFile(filepath.Join(depRecord.FilePath, "plugin.dll")); err != nil || string(data) != "dependency bytes" {
					t.Errorf("dependency bytes = %q, %v", data, err)
				}
				beforeChanges, beforeFiles := commandDatabaseChanges(t, db), commandTree(t, server)
				cmd := command.NewAddCommand(frameworks, ms)
				cmd.SetArgs([]string{"--namespace", root.Namespace, "--mod", root.Name})
				retryOutput := runOutcomeCommand(t, cmd)
				if ms.result != service.ErrModAlreadyInstalled || frameworks.result != service.ErrNoChanges ||
					!strings.Contains(retryOutput, "mod already installed") ||
					strings.Contains(retryOutput, "successfully") || strings.Contains(retryOutput, "warning:") {
					t.Errorf("healthy repeat was not a no-op: %v, %v, %q", frameworks.result, ms.result, retryOutput)
				}
				if commandDatabaseChanges(t, db) != beforeChanges || !reflect.DeepEqual(commandTree(t, server), beforeFiles) ||
					exactFetches != 2 || packages[dependency.Name] != 1 || downloads[dependency.DownloadURL] != 1 || downloads[root.DownloadURL] != 0 {
					t.Errorf("healthy repeat performed work: fetches=%v, downloads=%v", packages, downloads)
				}
			} else if !errors.Is(err, repo.ErrModFetchNoResults) {
				t.Errorf("unexpected dependency record: %+v, %v", depRecord, err)
			}
			if tt.existing {
				if _, err := os.Lstat(artifact); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("recorded framework unexpectedly bootstrapped: %v", err)
				}
			} else if data, err := os.ReadFile(artifact); err != nil || string(data) != "partial cleanup artifact" {
				t.Errorf("cleanup artifact was consumed: %q, %v", data, err)
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("unexpected backup artifacts: %v, %v", entries, err)
			}
		})
	}
}

func TestAddCommandBootstrapBoundary(t *testing.T) {
	for _, outcome := range []string{
		"completed", "already recorded", "completed cleanup", "cancelled", "file cleanup only",
		"nested completion", "nested no changes", "nested cancellation", "mod completion", "mod removal completion",
	} {
		t.Run("given "+outcome+" from bootstrap, when adding, then require framework readiness at the service boundary", func(t *testing.T) {
			cause := errors.New("bootstrap diagnostic")
			leaf := &file.FrameworkMutationCleanupError{Err: cause}
			completed := &service.FrameworkMutationCleanupError{Err: leaf}
			var result error
			wantAdd, wantWarning, wantDiagnostic := false, false, true
			switch outcome {
			case "completed":
				wantAdd, wantDiagnostic = true, false
			case "already recorded":
				result, wantAdd, wantDiagnostic = service.ErrNoChanges, true, false
			case "completed cleanup":
				result, wantAdd, wantWarning = completed, true, true
			case "cancelled":
				result, wantDiagnostic = service.ErrCancelled, false
			case "file cleanup only":
				result = leaf
			case "nested completion":
				result = errors.Join(service.ErrUnableToInstallFramework, completed)
			case "nested no changes":
				result = errors.Join(cause, service.ErrNoChanges)
			case "nested cancellation":
				result = errors.Join(cause, service.ErrCancelled)
			case "mod completion":
				result = &service.ModMutationCleanupError{Err: cause}
			case "mod removal completion":
				result = &service.ModRemovalCleanupError{Err: cause}
			}
			calls := 0
			ms := &completionModStub{add: func(namespace, name string) error {
				calls++
				if namespace != "Owner" || name != "Root" {
					t.Fatalf("unexpected mod request %s/%s", namespace, name)
				}
				return fmt.Errorf("%w: requested dependency unavailable", service.ErrAddDependenciesFailed)
			}}
			cmd := command.NewAddCommand(&standaloneFrameworkStub{install: func() error { return result }}, ms)
			cmd.SetArgs([]string{"--namespace", "Owner", "--mod", "Root"})
			output := runOutcomeCommand(t, cmd)
			wantCalls := 0
			if wantAdd {
				wantCalls = 1
			}
			if calls != wantCalls || strings.Contains(output, "warning:") != wantWarning || strings.Contains(output, "successfully") {
				t.Errorf("bootstrap boundary misclassified: calls=%d, output=%q", calls, output)
			}
			if wantDiagnostic && strings.Count(output, cause.Error()) != 1 {
				t.Errorf("bootstrap diagnostic was hidden or repeated: %q", output)
			}
			if wantAdd && strings.Count(output, "requested dependency unavailable") != 1 {
				t.Errorf("framework readiness hid the mod failure: %q", output)
			}
		})
	}
}

type observedOutcomeFramework struct {
	service.Framework
	result error
}

func (s *observedOutcomeFramework) InstallBepInEx() error {
	s.result = s.Framework.InstallBepInEx()
	return s.result
}
