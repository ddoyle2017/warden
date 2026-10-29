package command_test

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/command"
	"github.com/ddoyle2017/warden/internal/data/file"
	"github.com/ddoyle2017/warden/internal/data/repo"
	"github.com/ddoyle2017/warden/internal/domain/framework"
	"github.com/ddoyle2017/warden/internal/domain/mod"
	"github.com/ddoyle2017/warden/internal/service"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"strings"
	"testing"
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
			recordCause := errors.New("database driver lost write confirmation")
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
				return recordCause
			}
			r := &mock.ModsRepo{
				GetModByIdentityFunc: func(string, string) (mod.Mod, error) {
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
			output, err := runOutcomeCommand(t, cmd, args...)
			if tt.success || tt.committed {
				if err != nil {
					t.Errorf("completed removal returned failure: %v", err)
				}
			} else if !errors.Is(err, recordCause) || !errors.Is(err, service.ErrUnableToRemoveMod) {
				t.Errorf("command lost removal failure causes: %v", err)
			}
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

func TestFrameworkRemovalDiagnostics(t *testing.T) {
	for name, tt := range map[string]struct{ cleanup, failedFiles, nestedFramework bool }{
		"given metadata failure after file removal, when reporting, then show the mismatch and verification guidance":        {},
		"given cleanup and metadata failure after file removal, when reporting, then show both causes and the leftover path": {cleanup: true},
		"given a failed framework removal with nested mod cleanup, when reporting, then retain the enclosing failure":        {failedFiles: true},
		"given a failed framework removal with nested framework cleanup, when reporting, then retain the enclosing failure":  {failedFiles: true, nestedFramework: true},
	} {
		t.Run(name, func(t *testing.T) {
			plugins, retained := t.TempDir(), t.TempDir()
			fileCalls, recordCalls := 0, 0
			fm := &mock.Manager{
				BepInExPluginDirectoryFunc: func() string { return plugins },
				RemoveBepInExFunc: func() error {
					fileCalls++
					if tt.failedFiles {
						cleanupCause := fmt.Errorf("nested cleanup interrupted at %q", retained)
						var nested error = &file.ModRemovalCleanupError{Err: cleanupCause}
						if tt.nestedFramework {
							nested = &file.FrameworkMutationCleanupError{Err: cleanupCause}
						}
						return fmt.Errorf("%w: removal interrupted: %w", file.ErrFrameworkDeleteFailed, nested)
					}
					if tt.cleanup {
						return &file.FrameworkMutationCleanupError{Err: fmt.Errorf("cleanup interrupted at %q", retained)}
					}
					return nil
				},
			}
			fr := &mock.FrameworksRepo{DeleteFrameworkFunc: func(name, directory string) error {
				recordCalls++
				if name != framework.BepInEx || directory != plugins {
					t.Fatalf("unexpected removal scope: %q, %q", name, directory)
				}
				return errors.Join(repo.ErrFrameworkDeleteFailed, errors.New("record write interrupted"))
			}}
			fs := service.NewFrameworkService(fr, fm, &mock.Thunderstore{}, strings.NewReader("YES I AM\n"))
			cmd := command.NewRemoveCommand(fs, nil)
			output, err := runOutcomeCommand(t, cmd, "bepinex")
			if !errors.Is(err, service.ErrUnableToRemoveFramework) {
				t.Errorf("command lost framework removal failure: %v", err)
			}
			if !tt.failedFiles && !errors.Is(err, repo.ErrFrameworkDeleteFailed) {
				t.Errorf("command lost metadata failure: %v", err)
			}
			want := []string{"framework files were removed", "record write interrupted", "verify database state"}
			wantRecordCalls := 1
			if tt.failedFiles {
				want = []string{"unable to remove BepInEx", "removal interrupted", "nested cleanup interrupted", retained}
				wantRecordCalls = 0
				if strings.Contains(output, "framework files were removed") || strings.Contains(output, "warning: mod files removed") {
					t.Errorf("nested cleanup was classified as completed framework removal: %q", output)
				}
			}
			if tt.cleanup {
				want = append(want, "cleanup interrupted", retained)
			}
			for _, text := range want {
				if strings.Count(output, text) != 1 {
					t.Errorf("diagnostic %q was hidden or duplicated: %q", text, output)
				}
			}
			if fileCalls != 1 || recordCalls != wantRecordCalls || strings.Contains(output, "successfully") {
				t.Errorf("failed metadata handling was retried or reported as success: calls=%d/%d, output=%q", fileCalls, recordCalls, output)
			}
		})
	}
}

func TestRemovalCompletionBoundary(t *testing.T) {
	for _, all := range []bool{false, true} {
		for _, phase := range []string{"completed", "staging", "metadata", "rollback", "commit"} {
			t.Run(fmt.Sprintf("given %s cleanup with all=%t, when removing, then report only whole-request completion", phase, all), func(t *testing.T) {
				cause := errors.New("retained cleanup detail")
				marker := &file.ModRemovalCleanupError{Err: cause}
				recordCause := errors.New("record deletion uncertain")
				stages, writes, commits, rollbacks := 0, 0, 0, 0
				removal := &mock.ModRemoval{
					CommitFunc: func() error {
						commits++
						if phase == "completed" {
							return marker
						}
						if phase != "commit" {
							t.Fatal("committed a failed removal")
						}
						return errors.Join(file.ErrBackupDeleteFailed, marker)
					},
					RollbackFunc: func() error {
						rollbacks++
						if phase == "rollback" {
							return marker
						}
						return nil
					},
				}
				stage := func() (file.ModRemoval, error) {
					stages++
					if phase == "staging" {
						return nil, marker
					}
					return removal, nil
				}
				deleteRecords := func() error {
					writes++
					switch phase {
					case "metadata":
						return errors.Join(recordCause, marker)
					case "rollback":
						return recordCause
					default:
						return nil
					}
				}
				mr := &mock.ModsRepo{
					GetModByIdentityFunc: func(namespace, name string) (mod.Mod, error) {
						if namespace != "Owner" || name != "Root" {
							t.Fatalf("unexpected target %s/%s", namespace, name)
						}
						return mod.Mod{Namespace: namespace, Name: name, Version: "1.0.0"}, nil
					},
					DeleteModFunc: func(name, namespace string) error {
						if namespace != "Owner" || name != "Root" {
							t.Fatalf("unexpected deletion %s/%s", namespace, name)
						}
						return deleteRecords()
					},
					DeleteAllModsFunc: deleteRecords,
				}
				fm := &mock.Manager{
					StageModRemovalFunc: func(fullName string) (file.ModRemoval, error) {
						if fullName != "Owner-Root-1.0.0" {
							t.Fatalf("unexpected staged release %q", fullName)
						}
						return stage()
					},
					StageAllModsRemovalFunc: stage,
				}
				answer, args := "Y\n", []string{"--namespace", "Owner", "--mod", "Root"}
				if all {
					answer, args = "YES I AM\n", []string{"all"}
				}
				ms := &observedOutcomeRemoval{Mod: service.NewModService(mr, fm, &mock.Thunderstore{}, strings.NewReader(answer))}
				cmd := command.NewRemoveCommand(nil, ms)
				output, err := runOutcomeCommand(t, cmd, args...)
				completed := phase == "completed"
				wantErr := ms.result
				if completed {
					wantErr = nil
				}
				if !errors.Is(err, wantErr) {
					t.Errorf("command error = %v, want %v", err, wantErr)
				}
				if _, direct := ms.result.(*service.ModRemovalCleanupError); direct != completed {
					t.Errorf("service removal boundary misclassified: %v", ms.result)
				}
				var fileCompletion *file.ModRemovalCleanupError
				if !errors.As(ms.result, &fileCompletion) || !errors.Is(ms.result, marker) ||
					!errors.Is(ms.result, cause) || !errors.Is(ms.result, service.ErrUnableToRemoveMod) {
					t.Errorf("removal lost cause compatibility: %v", ms.result)
				}
				wantWrites, wantCommits, wantRollbacks := 1, 0, 0
				if phase == "staging" {
					wantWrites = 0
				} else if completed || phase == "commit" {
					wantCommits = 1
				} else {
					wantRollbacks = 1
					if !errors.Is(ms.result, recordCause) || !strings.Contains(output, "verify database state") {
						t.Errorf("metadata uncertainty was hidden: %v, %q", ms.result, output)
					}
				}
				if stages != 1 || writes != wantWrites || commits != wantCommits || rollbacks != wantRollbacks {
					t.Errorf("unexpected stages/writes/commits/rollbacks = %d/%d/%d/%d", stages, writes, commits, rollbacks)
				}
				if strings.Contains(output, "warning:") != completed || strings.Contains(output, "successfully") ||
					strings.Count(output, cause.Error()) != 1 {
					t.Errorf("removal outcome was misreported or repeated: %q", output)
				}
			})
		}
	}
}

type observedOutcomeRemoval struct {
	service.Mod
	result error
}

func (s *observedOutcomeRemoval) RemoveMod(namespace, name string) error {
	s.result = s.Mod.RemoveMod(namespace, name)
	return s.result
}

func (s *observedOutcomeRemoval) RemoveAllMods() error {
	s.result = s.Mod.RemoveAllMods()
	return s.result
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
