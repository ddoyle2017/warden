package command_test

import (
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/command"
	"github.com/ddoyle2017/warden/internal/data/file"
	"github.com/ddoyle2017/warden/internal/service"
	"path/filepath"
	"strings"
	"testing"
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
				var args []string
				switch operation {
				case "add":
					cmd = command.NewAddCommand(&standaloneFrameworkStub{install: func() error { return nil }}, ms)
					args = []string{"--namespace", "Owner", "--mod", "Root"}
				case "single update":
					args = []string{"--mod", "Root"}
				case "qualified update":
					args = []string{"--namespace", "Owner", "--mod", "Root"}
				case "bulk update":
					args = []string{"all"}
				}
				output, err := runOutcomeCommand(t, cmd, args...)
				wantErr := result
				if outcome == "completed cleanup" {
					wantErr = nil
				}
				if !errors.Is(err, wantErr) {
					t.Errorf("command error = %v, want %v", err, wantErr)
				}
				if calls != 1 {
					t.Errorf("service calls = %d, want 1", calls)
				}
				for _, text := range want {
					if strings.Count(output, text) != 1 {
						t.Errorf("output must contain %q exactly once: %q", text, output)
					}
				}
				if strings.HasPrefix(output, "... warning:") != (outcome == "completed cleanup") {
					t.Errorf("whole-request warning status misclassified: %q", output)
				}
				if strings.Contains(output, "successfully") {
					t.Errorf("diagnostic reported as ordinary success: %q", output)
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
