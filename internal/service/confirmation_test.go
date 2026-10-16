package service_test

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/mock"
)

func TestConfirmationInput(t *testing.T) {
	readFailure := errors.New("confirmation reader interrupted")
	operations := []struct {
		name, yes, no string
		before, work  []string
	}{
		{
			name: "update mod", yes: "Y", no: "n",
			before: []string{"get mod", "get package"},
			work:   []string{"validate install", "remove mod files", "install mod files", "save mod"},
		},
		{
			name: "update mod by identity", yes: "Y", no: "n",
			before: []string{"get mod", "get package"},
			work:   []string{"validate install", "remove mod files", "install mod files", "save mod"},
		},
		{
			name: "update all mods", yes: "Y", no: "n",
			work: []string{"list mods", "get package", "validate install", "remove mod files", "install mod files", "save mod"},
		},
		{
			name: "remove mod", yes: "Y", no: "n",
			work: []string{"stage mod"},
		},
		{
			name: "remove all mods", yes: "YES I AM", no: "no",
			work: []string{"stage all mods", "delete all mods", "commit removal"},
		},
		{
			name: "install framework", yes: "Y", no: "n",
			before: []string{"get framework"},
			work:   []string{"get package", "install framework files", "insert framework"},
		},
		{
			name: "update framework", yes: "Y", no: "n",
			before: []string{"get framework", "get package"},
			work:   []string{"update framework files", "update framework record"},
		},
		{
			name: "remove framework", yes: "YES I AM", no: "no",
			work: []string{"remove framework files", "get plugin directory", "delete framework"},
		},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			otherYes, otherNo := "YES I AM", "no"
			if operation.yes == otherYes {
				otherYes, otherNo = "Y", "n"
			}
			tests := map[string]struct {
				steps     []confirmationRead
				wantErr   error
				wantReads int
				accepted  bool
			}{
				"given an affirmative answer, when confirming, then perform the work": {
					steps: []confirmationRead{{text: operation.yes + "\n"}}, wantReads: 1, accepted: true,
				},
				"given a negative answer, when confirming, then leave the installation unchanged": {
					steps: []confirmationRead{{text: operation.no + "\n"}}, wantReads: 1,
				},
				"given invalid then affirmative answers, when confirming, then perform the work": {
					steps: []confirmationRead{{text: "invalid\n"}, {text: operation.yes + "\n"}}, wantReads: 2, accepted: true,
				},
				"given invalid then negative answers, when confirming, then leave the installation unchanged": {
					steps: []confirmationRead{{text: "invalid\n"}, {text: operation.no + "\n"}}, wantReads: 2,
				},
				"given two invalid answers, when exhausted, then stop before a failing third read": {
					steps: []confirmationRead{{text: "invalid\n"}, {text: "invalid\n"}}, wantErr: service.ErrMaxAttempts, wantReads: 2,
				},
				"given a third affirmative answer, when two attempts are exhausted, then do not read it": {
					steps:   []confirmationRead{{text: "invalid\n"}, {text: "invalid\n"}, {text: operation.yes + "\n"}},
					wantErr: service.ErrMaxAttempts, wantReads: 2,
				},
				"given differently capitalized answers, when confirming, then reject both": {
					steps:   []confirmationRead{{text: strings.ToLower(operation.yes) + "\n"}, {text: strings.ToUpper(operation.no) + "\n"}},
					wantErr: service.ErrMaxAttempts, wantReads: 2,
				},
				"given padded answers, when confirming, then reject both": {
					steps:   []confirmationRead{{text: " " + operation.yes + "\n"}, {text: operation.no + " \n"}},
					wantErr: service.ErrMaxAttempts, wantReads: 2,
				},
				"given the other prompt's tokens, when confirming, then reject both": {
					steps:   []confirmationRead{{text: otherYes + "\n"}, {text: otherNo + "\n"}},
					wantErr: service.ErrMaxAttempts, wantReads: 2,
				},
				"given an initial reader failure, when confirming, then preserve its cause without work": {
					steps: []confirmationRead{{err: readFailure}}, wantErr: readFailure, wantReads: 1,
				},
				"given a reader failure after invalid input, when confirming, then preserve its cause without work": {
					steps: []confirmationRead{{text: "invalid\n"}, {err: readFailure}}, wantErr: readFailure, wantReads: 2,
				},
				"given an incomplete affirmative answer with a reader failure, when confirming, then do not authorize work": {
					steps: []confirmationRead{{text: operation.yes, err: readFailure}}, wantErr: readFailure, wantReads: 1,
				},
				"given a complete affirmative answer with a reader failure, when confirming, then report the failure without work": {
					steps: []confirmationRead{{text: operation.yes + "\n", err: readFailure}}, wantErr: readFailure, wantReads: 1,
				},
				"given a negative answer with a reader failure, when confirming, then preserve the failure without work": {
					steps: []confirmationRead{{text: operation.no + "\n", err: readFailure}}, wantErr: readFailure, wantReads: 1,
				},
				"given a second invalid answer with a reader failure, when confirming, then preserve the failure without work": {
					steps: []confirmationRead{{text: "invalid\n"}, {text: "invalid\n", err: readFailure}}, wantErr: readFailure, wantReads: 2,
				},
				"given immediate EOF, when confirming, then report EOF without work": {
					steps: []confirmationRead{{err: io.EOF}}, wantErr: io.EOF, wantReads: 1,
				},
				"given EOF after invalid input, when confirming, then report EOF without work": {
					steps: []confirmationRead{{text: "invalid\n"}, {err: io.EOF}}, wantErr: io.EOF, wantReads: 2,
				},
				"given a final affirmative answer without a newline, when confirming, then perform the work": {
					steps: []confirmationRead{{text: operation.yes, err: io.EOF}}, wantReads: 1, accepted: true,
				},
				"given a final negative answer without a newline, when confirming, then leave the installation unchanged": {
					steps: []confirmationRead{{text: operation.no, err: io.EOF}}, wantReads: 1,
				},
				"given two invalid answers ending at EOF, when confirming, then report the attempt limit": {
					steps: []confirmationRead{{text: "invalid\n"}, {text: "invalid", err: io.EOF}}, wantErr: service.ErrMaxAttempts, wantReads: 2,
				},
				"given an oversized answer, when scanning fails, then preserve the scanner error without work": {
					steps: []confirmationRead{{text: strings.Repeat("x", bufio.MaxScanTokenSize)}}, wantErr: bufio.ErrTooLong,
				},
			}
			for name, tt := range tests {
				t.Run(name, func(t *testing.T) {
					reader := &confirmationReader{steps: tt.steps}
					var calls []string
					run := confirmationOperation(t, operation.name, reader, &calls, tt.accepted)
					err := run()
					if !errors.Is(err, tt.wantErr) {
						t.Errorf("confirmation error = %v, want %v", err, tt.wantErr)
					}
					wantInputError := tt.wantErr != nil && tt.wantErr != service.ErrMaxAttempts
					if errors.Is(err, service.ErrConfirmationInput) != wantInputError {
						t.Errorf("incorrect confirmation input error classification: %v", err)
					}
					if tt.wantReads > 0 && reader.reads != tt.wantReads {
						t.Errorf("input reads = %d, want %d", reader.reads, tt.wantReads)
					}
					wantCalls := append([]string(nil), operation.before...)
					if tt.accepted {
						wantCalls = append(wantCalls, operation.work...)
					}
					if !slices.Equal(calls, wantCalls) {
						t.Errorf("operation calls = %v, want %v", calls, wantCalls)
					}
				})
			}
		})
	}
}

type confirmationRead struct {
	text string
	err  error
}

type confirmationReader struct {
	steps []confirmationRead
	reads int
}

func (r *confirmationReader) Read(p []byte) (int, error) {
	r.reads++
	if len(r.steps) == 0 {
		return 0, errors.New("unexpected read beyond supplied confirmation input")
	}
	// Separate reads prevent the scanner from prefetching the next answer.
	step := &r.steps[0]
	n := copy(p, step.text)
	step.text = step.text[n:]
	if step.text != "" {
		return n, nil
	}
	err := step.err
	r.steps = r.steps[1:]
	return n, err
}

func confirmationOperation(t *testing.T, operation string, reader io.Reader, calls *[]string, wantWork bool) func() error {
	t.Helper()
	record := func(call string) { *calls = append(*calls, call) }
	current := mod.Mod{Name: "Plugin", Namespace: "Owner", Version: "1.0.0"}
	latest := thunderstore.Release{
		Name: current.Name, Namespace: current.Namespace, VersionNumber: "2.0.0",
		FullName: "Owner-Plugin-2.0.0", DownloadURL: "https://example.invalid/plugin.zip",
	}
	mr, fm, ts := &mock.ModsRepo{}, &mock.Manager{}, &mock.Thunderstore{}
	getMod := func(name string) (mod.Mod, error) {
		if name != current.Name {
			t.Fatalf("unexpected mod lookup %q", name)
		}
		record("get mod")
		return current, nil
	}
	getPackage := func(namespace, name string) (thunderstore.Package, error) {
		if namespace != latest.Namespace || name != latest.Name {
			t.Fatalf("unexpected package lookup %q/%q", namespace, name)
		}
		record("get package")
		return thunderstore.Package{Latest: latest}, nil
	}
	checkRelease := func(url, fullName string) {
		t.Helper()
		if url != latest.DownloadURL || fullName != latest.FullName {
			t.Fatalf("unexpected release %q, %q", url, fullName)
		}
	}
	removal := &mock.ModRemoval{CommitFunc: func() error {
		record("commit removal")
		return nil
	}}
	ms := service.NewModService(mr, fm, ts, reader)
	switch operation {
	case "update mod", "update mod by identity", "update all mods":
		ts.GetPackageFunc = getPackage
		fm.ValidateModInstallFunc = func() error {
			record("validate install")
			return nil
		}
		fm.RemoveModFunc = func(fullName string) error {
			if fullName != current.FullName() {
				t.Fatalf("unexpected removal target %q", fullName)
			}
			record("remove mod files")
			return nil
		}
		fm.InstallModFunc = func(url, fullName string) (string, error) {
			checkRelease(url, fullName)
			record("install mod files")
			return "installed-plugin", nil
		}
		mr.UpsertModFunc = func(m mod.Mod) error {
			if m.Name != latest.Name || m.Namespace != latest.Namespace || m.Version != latest.VersionNumber || m.FilePath != "installed-plugin" {
				t.Fatalf("unexpected mod record %+v", m)
			}
			record("save mod")
			return nil
		}
		if operation == "update mod" {
			mr.GetModFunc = getMod
			return func() error { return ms.UpdateMod(current.Name) }
		}
		if operation == "update mod by identity" {
			mr.GetModByIdentityFunc = func(namespace, name string) (mod.Mod, error) {
				if namespace != current.Namespace {
					t.Fatalf("unexpected namespace %q", namespace)
				}
				return getMod(name)
			}
			return func() error { return ms.UpdateModByIdentity(current.Namespace, current.Name) }
		}
		mr.ListModsFunc = func() ([]mod.Mod, error) {
			record("list mods")
			return []mod.Mod{current}, nil
		}
		return ms.UpdateAllMods
	case "remove mod":
		th, stored, installed, backups := newUpdatePreflightFixture(t)
		manager := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())
		fm.StageModRemovalFunc = func(fullName string) (file.ModRemoval, error) {
			if fullName != installed.FullName() {
				t.Fatalf("unexpected removal target %q", fullName)
			}
			record("stage mod")
			return manager.StageModRemoval(fullName)
		}
		ms := service.NewModService(stored, fm, ts, reader)
		return func() error {
			result := ms.RemoveMod(installed.Namespace, installed.Name)
			records, err := stored.ListMods()
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := os.ReadFile(filepath.Join(installed.FilePath, "plugin.dll"))
			if wantWork {
				if len(records) != 0 {
					t.Errorf("confirmed removal kept records: %+v", records)
				}
				if !errors.Is(readErr, os.ErrNotExist) {
					t.Errorf("confirmed removal kept plugin bytes: %q, %v", data, readErr)
				}
			} else {
				if !reflect.DeepEqual(records, []mod.Mod{installed}) {
					t.Errorf("unconfirmed removal changed records: %+v, want %+v", records, installed)
				}
				if readErr != nil || string(data) != "installed bytes" {
					t.Errorf("unconfirmed removal changed plugin bytes: %q, %v", data, readErr)
				}
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("removal left backup artifacts: %v, %v", entries, err)
			}
			return result
		}
	case "remove all mods":
		fm.StageAllModsRemovalFunc = func() (file.ModRemoval, error) {
			record("stage all mods")
			return removal, nil
		}
		mr.DeleteAllModsFunc = func() error {
			record("delete all mods")
			return nil
		}
		return ms.RemoveAllMods
	}

	latest.Name, latest.Namespace = framework.BepInEx, framework.BepInExNamespace
	latest.FullName = "denikson-BepInExPack_Valheim-2.0.0"
	fr := &mock.FrameworksRepo{}
	fs := service.NewFrameworkService(fr, fm, ts, reader)
	if operation == "install framework" || operation == "update framework" {
		fr.GetFrameworkFunc = func(name string) (framework.Framework, error) {
			if name != framework.BepInEx {
				t.Fatalf("unexpected framework lookup %q", name)
			}
			record("get framework")
			if operation == "install framework" {
				return framework.Framework{}, repo.ErrFrameworkFetchNoResults
			}
			return framework.Framework{ID: 7, Name: latest.Name, Namespace: latest.Namespace, Version: "1.0.0"}, nil
		}
		ts.GetPackageFunc = getPackage
	}
	checkFramework := func(f framework.Framework) {
		t.Helper()
		if f.Name != latest.Name || f.Namespace != latest.Namespace || f.Version != latest.VersionNumber {
			t.Fatalf("unexpected framework record %+v", f)
		}
	}
	switch operation {
	case "install framework":
		fm.InstallBepInExFunc = func(url, fullName string) (string, error) {
			checkRelease(url, fullName)
			record("install framework files")
			return "installed-framework", nil
		}
		fr.InsertFrameworkFunc = func(f framework.Framework) error {
			checkFramework(f)
			record("insert framework")
			return nil
		}
		return fs.InstallBepInEx
	case "update framework":
		fm.UpdateBepInExFunc = func(url, fullName string) error {
			checkRelease(url, fullName)
			record("update framework files")
			return nil
		}
		fr.UpdateFrameworkFunc = func(f framework.Framework) error {
			checkFramework(f)
			if f.ID != 7 {
				t.Fatalf("unexpected framework ID %d", f.ID)
			}
			record("update framework record")
			return nil
		}
		return fs.UpdateBepInEx
	case "remove framework":
		plugins := filepath.Join(t.TempDir(), "BepInEx", "plugins")
		fm.RemoveBepInExFunc = func() error {
			record("remove framework files")
			return nil
		}
		fm.BepInExPluginDirectoryFunc = func() string {
			record("get plugin directory")
			return plugins
		}
		fr.DeleteFrameworkFunc = func(name, pluginDirectory string) error {
			if name != framework.BepInEx || pluginDirectory != plugins {
				t.Fatalf("unexpected framework deletion scope %q, %q", name, pluginDirectory)
			}
			record("delete framework")
			return nil
		}
		return fs.RemoveBepInEx
	default:
		t.Fatalf("unknown confirmation operation %q", operation)
		return nil
	}
}
