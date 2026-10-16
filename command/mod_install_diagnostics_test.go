package command_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/command"
	"warden/internal/data/file"
	"warden/internal/service"
)

func TestModInstallDiagnostics(t *testing.T) {
	for _, operation := range []string{"add", "single update", "qualified update", "bulk update"} {
		for _, outcome := range []string{
			"completed cleanup", "recovery failure", "record failure", "dependency failure", "nested request completion",
			"file completion only", "framework completion", "removal completion", "nested no changes", "nested already installed",
		} {
			t.Run("given "+outcome+", when reporting "+operation+", then preserve diagnostics and request status once", func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				artifact := filepath.Join(t.TempDir(), "backup-artifact")
				cleanup := &file.ModInstallCleanupError{Err: fmt.Errorf("cleanup interrupted at %q", artifact)}
				completed := &service.ModMutationCleanupError{Err: cleanup}
				sentinel := service.ErrModInstallFailed
				if operation != "add" {
					sentinel = service.ErrUnableToUpdateMod
				}
				var result error
				var want []string
				switch outcome {
				case "completed cleanup":
					result = completed
					want = []string{"mod request completed", "cleanup interrupted", artifact}
				case "recovery failure":
					result = fmt.Errorf("%w: %w; backup retained at %q", sentinel, file.ErrBackupRestoreFailed, artifact)
					want = []string{"unable to restore backup", "backup retained at", artifact}
				case "record failure":
					result = errors.Join(fmt.Errorf("%w: metadata write interrupted; verify database state", sentinel), cleanup)
					want = []string{"metadata write interrupted", "verify database state", "cleanup interrupted", artifact}
				case "dependency failure":
					result = errors.Join(fmt.Errorf("%w: dependency download interrupted", service.ErrAddDependenciesFailed), cleanup)
					want = []string{"dependency download interrupted", "cleanup interrupted", artifact}
				case "nested request completion":
					result = errors.Join(fmt.Errorf("%w: later operation interrupted", sentinel), completed)
					want = []string{"later operation interrupted", "cleanup interrupted", artifact}
				case "file completion only":
					result = cleanup
					want = []string{"mod files installed", "cleanup interrupted", artifact}
				case "framework completion":
					result = &service.FrameworkMutationCleanupError{Err: cleanup}
					want = []string{"framework request completed", "cleanup interrupted", artifact}
				case "removal completion":
					result = &service.ModRemovalCleanupError{Err: cleanup}
					want = []string{"mod removal completed", "cleanup interrupted", artifact}
				case "nested no changes":
					result = errors.Join(fmt.Errorf("%w: lookup interrupted", sentinel), service.ErrNoChanges)
					want = []string{"lookup interrupted", service.ErrNoChanges.Error()}
				case "nested already installed":
					result = errors.Join(fmt.Errorf("%w: lookup interrupted", sentinel), service.ErrModAlreadyInstalled)
					want = []string{"lookup interrupted", service.ErrModAlreadyInstalled.Error()}
				}
				calls := 0
				run := func(mode, namespace, name string) error {
					calls++
					if mode != operation || (mode != "bulk update" && name != "Root") ||
						((mode == "add" || mode == "qualified update") && namespace != "Owner") {
						t.Fatalf("unexpected request %q/%q/%q", mode, namespace, name)
					}
					return result
				}
				ms := &modDiagnosticsStub{
					add:       func(ns, name string) error { return run("add", ns, name) },
					update:    func(name string) error { return run("single update", "", name) },
					qualified: func(ns, name string) error { return run("qualified update", ns, name) },
					all:       func() error { return run("bulk update", "", "") },
				}
				cmd := command.NewUpdateCommand(nil, ms)
				switch operation {
				case "add":
					cmd = command.NewAddCommand(&standaloneFrameworkStub{install: func() error { return nil }}, ms)
					cmd.SetArgs([]string{"--namespace", "Owner", "--mod", "Root"})
				case "single update":
					cmd.SetArgs([]string{"--mod", "Root"})
				case "qualified update":
					cmd.SetArgs([]string{"--namespace", "Owner", "--mod", "Root"})
				case "bulk update":
					cmd.SetArgs([]string{"all"})
				}
				output, err := os.CreateTemp(t.TempDir(), "stdout")
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
				data, err := os.ReadFile(output.Name())
				if err != nil {
					t.Fatal(err)
				}
				if calls != 1 {
					t.Errorf("service calls = %d, want 1", calls)
				}
				for _, text := range want {
					if strings.Count(string(data), text) != 1 {
						t.Errorf("output must contain %q exactly once: %q", text, data)
					}
				}
				if strings.HasPrefix(string(data), "... warning:") != (outcome == "completed cleanup") {
					t.Errorf("whole-request warning status misclassified: %q", data)
				}
				if strings.Contains(string(data), "successfully") {
					t.Errorf("diagnostic reported as ordinary success: %q", data)
				}
			})
		}
	}
}

type modDiagnosticsStub struct {
	service.Mod
	add       func(string, string) error
	update    func(string) error
	qualified func(string, string) error
	all       func() error
}

func (s *modDiagnosticsStub) AddMod(namespace, name string) error { return s.add(namespace, name) }
func (s *modDiagnosticsStub) UpdateMod(name string) error         { return s.update(name) }
func (s *modDiagnosticsStub) UpdateModByIdentity(namespace, name string) error {
	return s.qualified(namespace, name)
}
func (s *modDiagnosticsStub) UpdateAllMods() error { return s.all() }
