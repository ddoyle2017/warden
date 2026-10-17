package command_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"warden/command"
	"warden/internal/data/file"
	"warden/internal/service"
)

func TestFrameworkUpdateDiagnostics(t *testing.T) {
	for name, tt := range map[string]struct{ completed, recordFailure, failed bool }{
		"given completed files with cleanup diagnostics, when updating, then print the recovery path": {completed: true},
		"given cleanup and metadata failures, when updating, then print both causes":                  {completed: true, recordFailure: true},
		"given a metadata failure after completed files, when updating, then print its cause":         {recordFailure: true},
		"given an incomplete update, when reporting failure, then retain the existing message":        {failed: true},
		"given a successful update, when returning, then retain the existing success message":         {},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "plugin-snapshot")
			var result error
			want := []string{"...successfully updated BepInEx!..."}
			if tt.completed {
				result = fmt.Errorf("%w: %w", service.ErrUnableToUpdateFramework, &file.FrameworkMutationCleanupError{
					Err: fmt.Errorf("snapshot cleanup interrupted at %q", path),
				})
				want = []string{"framework files changed", "snapshot cleanup interrupted", path}
			} else if tt.failed {
				result = service.ErrUnableToUpdateFramework
				want = []string{"unable to update BepInEx"}
			}
			if tt.recordFailure {
				result = errors.Join(result, fmt.Errorf("%w: framework record write interrupted", service.ErrUnableToInstallFramework))
				if !tt.completed {
					want = nil
				}
				want = append(want, "framework record write interrupted")
			}
			if tt.completed && !tt.recordFailure {
				result = &service.FrameworkMutationCleanupError{Err: result}
				want = append(want, "framework request completed")
			}
			calls := 0
			fs := &updateFrameworkStub{update: func() error {
				calls++
				return result
			}}
			cmd := command.NewUpdateCommand(fs, nil)
			output, err := runOutcomeCommand(t, cmd, "bepinex")
			wantErr := result
			if tt.completed && !tt.recordFailure {
				wantErr = nil
			}
			if !errors.Is(err, wantErr) {
				t.Errorf("command error = %v, want %v", err, wantErr)
			}
			if calls != 1 {
				t.Errorf("framework update calls = %d, want 1", calls)
			}
			for _, text := range want {
				if strings.Count(output, text) != 1 {
					t.Errorf("output must contain %q once: %q", text, output)
				}
			}
			if strings.HasPrefix(output, "... warning:") != (tt.completed && !tt.recordFailure) {
				t.Errorf("framework request status was misclassified: %q", output)
			}
			if result != nil && strings.Contains(output, "successfully") {
				t.Errorf("diagnostics reported as ordinary success: %q", output)
			}
		})
	}
}

type updateFrameworkStub struct {
	service.Framework
	update func() error
}

func (f *updateFrameworkStub) UpdateBepInEx() error {
	return f.update()
}

func TestUpdateVersionDiagnostics(t *testing.T) {
	for name, tt := range map[string]struct {
		args      []string
		operation string
		sentinel  error
	}{
		"given an invalid version, when updating one mod, then print the validation cause": {
			args: []string{"--mod", "Versioned"}, operation: "single", sentinel: service.ErrUnableToUpdateMod,
		},
		"given an invalid version, when updating all mods, then print the validation cause": {
			args: []string{"all"}, operation: "all", sentinel: service.ErrUnableToUpdateMod,
		},
		"given an invalid version, when updating BepInEx, then print the validation cause": {
			args: []string{"bepinex"}, operation: "framework", sentinel: service.ErrUnableToUpdateFramework,
		},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			result := func(operation string) error {
				calls++
				if operation != tt.operation {
					t.Fatalf("unexpected update operation %q", operation)
				}
				return fmt.Errorf("%w: %w: latest version %q", tt.sentinel, service.ErrInvalidReleaseVersion, "1.2")
			}
			ms := &updateModStub{
				update: func(name string) error {
					if name != "Versioned" {
						t.Fatalf("unexpected mod name %q", name)
					}
					return result("single")
				},
				updateAll: func() error { return result("all") },
			}
			fs := &updateFrameworkStub{update: func() error { return result("framework") }}
			cmd := command.NewUpdateCommand(fs, ms)
			output, err := runOutcomeCommand(t, cmd, tt.args...)
			if !errors.Is(err, tt.sentinel) || !errors.Is(err, service.ErrInvalidReleaseVersion) {
				t.Errorf("command lost version error causes: %v", err)
			}
			if calls != 1 {
				t.Errorf("update calls = %d, want 1", calls)
			}
			if strings.Count(output, `invalid release version: latest version "1.2"`) != 1 {
				t.Errorf("output omits or duplicates the validation cause: %q", output)
			}
			if strings.Contains(output, "successfully") {
				t.Errorf("failed update reported success: %q", output)
			}
		})
	}
}

type updateModStub struct {
	service.Mod
	update    func(string) error
	updateAll func() error
}

func (m *updateModStub) UpdateMod(name string) error { return m.update(name) }
func (m *updateModStub) UpdateAllMods() error        { return m.updateAll() }
