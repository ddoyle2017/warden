package command_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/command"
	"warden/internal/data/file"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/mock"
)

func TestRemoveCommand_Diagnostics(t *testing.T) {
	tests := map[string]struct {
		all       bool
		committed bool
		success   bool
	}{
		"given a single removal recovery failure, when reporting, then retain its instructions and path": {},
		"given an all-mod recovery failure, when reporting, then retain its instructions and path":       {all: true},
		"given a single committed removal, when cleanup fails, then report completion with a warning":    {committed: true},
		"given all removals committed, when cleanup fails, then report completion with a warning":        {all: true, committed: true},
		"given a successful single removal, when reporting, then retain its success message":             {success: true},
		"given successful removal of all mods, when reporting, then retain its success message":          {all: true, success: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			retained := t.TempDir()
			removal := &mock.ModRemoval{
				CommitFunc: func() error {
					if tt.success {
						return nil
					}
					return &file.ModRemovalCleanupError{Err: fmt.Errorf("cleanup leftovers at %q", retained)}
				},
				RollbackFunc: func() error {
					return fmt.Errorf("%w; original mods remain at %q", file.ErrBackupRestoreFailed, retained)
				},
			}
			deleteRecord := func() error {
				if tt.committed || tt.success {
					return nil
				}
				return errors.New("database driver lost write confirmation")
			}
			r := &mock.ModsRepo{
				GetModFunc: func(string) (mod.Mod, error) {
					return mod.Mod{Namespace: "Owner", Name: "Installed", Version: "1.0.0"}, nil
				},
				DeleteModFunc:     func(string, string) error { return deleteRecord() },
				DeleteAllModsFunc: deleteRecord,
			}
			fm := &mock.Manager{
				StageModRemovalFunc:     func(string) (file.ModRemoval, error) { return removal, nil },
				StageAllModsRemovalFunc: func() (file.ModRemoval, error) { return removal, nil },
			}
			answer := "Y"
			args := []string{"--namespace", "Owner", "--mod", "Installed"}
			if tt.all {
				answer = "YES I AM"
				args = []string{"all"}
			}
			ms := service.NewModService(r, fm, &mock.Thunderstore{}, strings.NewReader(answer))
			cmd := command.NewRemoveCommand(nil, ms)
			cmd.SetArgs(args)
			outputPath := filepath.Join(t.TempDir(), "output")
			out, err := os.Create(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			stdout := os.Stdout
			t.Cleanup(func() {
				os.Stdout = stdout
				if err := out.Close(); err != nil {
					t.Error(err)
				}
			})
			os.Stdout = out
			cmd.SetOut(out)
			cmd.SetErr(out)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			os.Stdout = stdout
			data, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			output := string(data)
			if tt.success {
				want := "mod successfully removed"
				if tt.all {
					want = "all mods were removed successfully"
				}
				if !strings.Contains(output, want) {
					t.Errorf("success message changed: %q", output)
				}
				return
			}
			if !strings.Contains(output, retained) {
				t.Errorf("diagnostic path was hidden: %q", output)
			}
			if tt.committed {
				if !strings.Contains(output, "warning") || !strings.Contains(output, "mod files removed") || !strings.Contains(output, "cleanup") || strings.Contains(output, "unable to remove") {
					t.Errorf("committed cleanup was reported as a failed removal: %q", output)
				}
			} else if !strings.Contains(output, "verify database state") || !strings.Contains(output, "original mods remain") || !strings.Contains(output, "database driver lost write confirmation") {
				t.Errorf("recovery instructions were hidden: %q", output)
			}
		})
	}
}

func TestRemoveCommand_RequiresTargetFlags(t *testing.T) {
	tests := map[string]struct {
		args    []string
		missing string
	}{
		"given no namespace, when removing a mod, then reject the input": {args: []string{"--mod", "Installed"}, missing: "namespace"},
		"given no mod name, when removing a mod, then reject the input":  {args: []string{"--namespace", "Owner"}, missing: "mod"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			cmd := command.NewRemoveCommand(nil, nil)
			cmd.SetArgs(tt.args)
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tt.missing) {
				t.Errorf("missing flag was not rejected: %v", err)
			}
		})
	}
}
