package command_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"warden/command"
	"warden/internal/config"
	"warden/internal/data/file"
	"warden/internal/domain/mod"
	"warden/internal/service"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestCommandRuntimeFailures(t *testing.T) {
	for _, operation := range []string{
		"add", "bootstrap", "update mod", "update qualified", "update all", "update framework",
		"remove mod", "remove all", "remove framework", "list", "start",
	} {
		t.Run("given an unexpected service failure, when executing "+operation+", then return its causes and report it once", func(t *testing.T) {
			cause := &os.PathError{Op: "write", Path: filepath.Join(t.TempDir(), "metadata"), Err: io.ErrUnexpectedEOF}
			result := fmt.Errorf("injected operation failure: %w", cause)
			calls := 0
			cmd, args := runtimeCommand(t, operation, func() error {
				calls++
				return result
			})
			output, err := runOutcomeCommand(t, cmd, args...)
			var pathError *os.PathError
			if !errors.Is(err, result) || !errors.Is(err, io.ErrUnexpectedEOF) ||
				!errors.As(err, &pathError) || pathError != cause {
				t.Errorf("command lost public error causes: %v", err)
			}
			if calls != 1 || strings.Count(output, "Error:") != 1 ||
				strings.Count(output, result.Error()) != 1 || strings.Contains(output, "Usage:") {
				t.Errorf("unexpected runtime reporting: calls=%d, output=%q", calls, output)
			}
			for _, forbidden := range []string{"successfully", "no mods are installed", "untrusted partial row", "warning:"} {
				if strings.Contains(output, forbidden) {
					t.Errorf("failed request produced %q: %q", forbidden, output)
				}
			}
		})
	}
}

func TestCommandSuccessOutput(t *testing.T) {
	for operation, want := range map[string]string{
		"add":              "... successfully installed mod! ...\n",
		"update mod":       "... mod successfully updated! ...\n",
		"update qualified": "... mod successfully updated! ...\n",
		"update all":       "",
		"update framework": "...successfully updated BepInEx!...\n",
		"remove mod":       "... mod successfully removed! ...\n",
		"remove all":       "... all mods were removed successfully! ...\n",
		"remove framework": "... BepInEx and mods were removed successfully! ...\n",
		"start":            "... successfully started server! ...\n\n",
	} {
		t.Run("given a completed service request, when executing "+operation+", then preserve successful output", func(t *testing.T) {
			calls := 0
			cmd, args := runtimeCommand(t, operation, func() error {
				calls++
				return nil
			})
			output, err := runOutcomeCommand(t, cmd, args...)
			if err != nil || calls != 1 || output != want {
				t.Errorf("successful output changed: calls=%d, error=%v, output=%q, want=%q", calls, err, output, want)
			}
		})
	}
}

func TestDependencyValidationDiagnostics(t *testing.T) {
	for _, operation := range []string{"add", "update mod", "update qualified", "update all"} {
		t.Run("given an invalid dependency version, when executing "+operation+", then preserve root token and validation causes", func(t *testing.T) {
			result := fmt.Errorf("%w: Owner/Root: dependency %q: %w: %w",
				service.ErrAddDependenciesFailed, "Required-Dependency-v1.2.3",
				service.ErrInvalidDependency, service.ErrInvalidReleaseVersion)
			calls := 0
			cmd, args := runtimeCommand(t, operation, func() error {
				calls++
				return result
			})
			output, err := runOutcomeCommand(t, cmd, args...)
			for _, cause := range []error{service.ErrAddDependenciesFailed, service.ErrInvalidDependency, service.ErrInvalidReleaseVersion} {
				if !errors.Is(err, cause) {
					t.Errorf("command lost dependency validation cause %v: %v", cause, err)
				}
			}
			if calls != 1 || strings.Count(output, result.Error()) != 1 || strings.Count(output, "Error:") != 1 ||
				strings.Contains(output, "Usage:") || strings.Contains(output, "successfully") {
				t.Errorf("dependency validation was hidden or duplicated: calls=%d, output=%q", calls, output)
			}
		})
	}
}

func TestDirectServiceOutcomes(t *testing.T) {
	all := map[string]bool{
		"add": true, "update mod": true, "update qualified": true, "update all": true,
		"remove mod": true, "remove all": true, "update framework": true, "remove framework": true,
	}
	mutations := map[string]bool{"add": true, "update mod": true, "update qualified": true, "update all": true}
	removals := map[string]bool{"remove mod": true, "remove all": true}
	frameworks := map[string]bool{"update framework": true, "remove framework": true}
	cause := errors.New("owned cleanup artifact remains")
	modFiles := &file.ModInstallCleanupError{Err: cause}
	removedFiles := &file.ModRemovalCleanupError{Err: cause}
	frameworkFiles := &file.FrameworkMutationCleanupError{Err: cause}
	for name, tt := range map[string]struct {
		result   error
		nonfatal map[string]bool
		warning  bool
	}{
		"cancellation":         {result: service.ErrCancelled, nonfatal: all},
		"no changes":           {result: service.ErrNoChanges, nonfatal: all},
		"already installed":    {result: service.ErrModAlreadyInstalled, nonfatal: map[string]bool{"add": true}},
		"mod completion":       {result: &service.ModMutationCleanupError{Err: modFiles}, nonfatal: mutations, warning: true},
		"removal completion":   {result: &service.ModRemovalCleanupError{Err: removedFiles}, nonfatal: removals, warning: true},
		"framework completion": {result: &service.FrameworkMutationCleanupError{Err: frameworkFiles}, nonfatal: frameworks, warning: true},
		"mod files only":       {result: modFiles},
		"removal files only":   {result: removedFiles},
		"framework files only": {result: frameworkFiles},
	} {
		for _, nesting := range []string{"direct", "wrapped", "joined"} {
			for operation := range all {
				t.Run("given "+nesting+" "+name+", when executing "+operation+", then classify only whole-service outcomes", func(t *testing.T) {
					result := tt.result
					switch nesting {
					case "wrapped":
						result = fmt.Errorf("request incomplete: %w", result)
					case "joined":
						result = errors.Join(errors.New("request incomplete"), result)
					}
					calls := 0
					cmd, args := runtimeCommand(t, operation, func() error {
						calls++
						return result
					})
					output, err := runOutcomeCommand(t, cmd, args...)
					nonfatal := nesting == "direct" && tt.nonfatal[operation]
					wantWarnings, wantErrors := 0, 1
					if nonfatal {
						wantErrors = 0
						if err != nil {
							t.Errorf("direct benign result returned failure: %v", err)
						}
						if tt.warning {
							wantWarnings = 1
						}
					} else if !errors.Is(err, result) || strings.Count(output, result.Error()) != 1 {
						t.Errorf("incomplete or wrong-boundary outcome was lost: error=%v, output=%q", err, output)
					}
					if calls != 1 || strings.Count(output, "Error:") != wantErrors ||
						strings.Count(output, "warning:") != wantWarnings ||
						strings.Contains(output, "successfully") || strings.Contains(output, "Usage:") {
						t.Errorf("incorrect request outcome: calls=%d, error=%v, output=%q", calls, err, output)
					}
					if wantWarnings == 1 && strings.Count(output, result.Error()) != 1 {
						t.Errorf("completed cleanup diagnostic was hidden or repeated: %q", output)
					}
					if nonfatal && name == "already installed" && strings.Count(output, "mod already installed") != 1 {
						t.Errorf("healthy no-op omitted its existing message: %q", output)
					}
				})
			}
		}
	}
}

func TestStartCommandDiagnostics(t *testing.T) {
	scriptFailure := &os.PathError{Op: "open", Path: filepath.Join(t.TempDir(), "start_server.sh"), Err: os.ErrNotExist}
	processFailure := &exec.Error{Name: "owned fixture", Err: io.ErrUnexpectedEOF}
	for name, tt := range map[string]struct {
		args          []string
		valid         bool
		result        error
		wantCause     error
		diagnostic    string
		validations   int
		starts, usage int
	}{
		"given no game type, when starting, then reject the argument count without service work": {
			diagnostic: "accepts 1 arg(s)", usage: 1,
		},
		"given extra game types, when starting, then reject the argument count without service work": {
			args: []string{"vanilla", "modded"}, diagnostic: "accepts 1 arg(s)", usage: 1,
		},
		"given an invalid game type, when starting, then retain the invalid-type cause without launching": {
			args: []string{"invalid"}, diagnostic: "invalid game type", wantCause: service.ErrInvalidGameType,
			validations: 1, usage: 1,
		},
		"given a missing script for a valid type, when starting, then retain the script failure rather than invalid type": {
			args: []string{"vanilla"}, valid: true, validations: 1, starts: 1,
			result: fmt.Errorf("%w: %w", service.ErrServerStartFailed, scriptFailure), wantCause: scriptFailure,
			diagnostic: "Valheim server failed to start",
		},
		"given a process failure for a valid type, when starting, then retain the process cause and output": {
			args: []string{"modded"}, valid: true, validations: 1, starts: 1,
			result: fmt.Errorf("%w: %w", service.ErrServerStartFailed, processFailure), wantCause: processFailure,
			diagnostic: "Valheim server failed to start",
		},
		"given a runtime invalid-type cause, when starting, then report it without a runtime usage dump": {
			args: []string{"modded"}, valid: true, validations: 1, starts: 1,
			result: fmt.Errorf("executor rejected type: %w", service.ErrInvalidGameType), wantCause: service.ErrInvalidGameType,
			diagnostic: "invalid game type",
		},
	} {
		t.Run(name, func(t *testing.T) {
			validations, starts := 0, 0
			server := &serverCommandStub{
				valid: func(gameType string) bool {
					validations++
					if gameType != tt.args[0] {
						t.Fatalf("unexpected game type %q", gameType)
					}
					return tt.valid
				},
				start: func(gameType string) (string, error) {
					starts++
					if gameType != tt.args[0] {
						t.Fatalf("unexpected game type %q", gameType)
					}
					return "owned process output", tt.result
				},
			}
			output, err := runOutcomeCommand(t, command.NewStartCommand(server), tt.args...)
			if err == nil || (tt.wantCause != nil && !errors.Is(err, tt.wantCause)) {
				t.Errorf("start lost its failure cause: %v", err)
			}
			if tt.result != nil && !errors.Is(err, tt.result) {
				t.Errorf("runtime start failure was replaced: %v", err)
			}
			if tt.wantCause == scriptFailure {
				var pathError *os.PathError
				if !errors.As(err, &pathError) || pathError != scriptFailure {
					t.Errorf("script cause is no longer inspectable: %v", err)
				}
			}
			if tt.wantCause == processFailure {
				var execError *exec.Error
				if !errors.As(err, &execError) || execError != processFailure {
					t.Errorf("process cause is no longer inspectable: %v", err)
				}
			}
			if validations != tt.validations || starts != tt.starts ||
				strings.Count(output, tt.diagnostic) != 1 || strings.Count(output, "Error:") != 1 ||
				strings.Count(output, "Usage:") != tt.usage || strings.Count(output, "owned process output") != tt.starts ||
				strings.Contains(output, "successfully") {
				t.Errorf("unexpected start result: validations=%d, starts=%d, output=%q", validations, starts, output)
			}
			if tt.diagnostic != "invalid game type" && strings.Contains(output, "invalid game type") {
				t.Errorf("valid type was misdiagnosed: %q", output)
			}
		})
	}
}

func TestConfigCommandPersistence(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("given a config write failure of %t, when setting a value, then report the persisted outcome", fail), func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			home := t.TempDir()
			path := filepath.Join(home, ".warden.yaml")
			writeCommandFile(t, path, "valheim-directory: original\nplatform: linux\n")
			viper.SetConfigFile(path)
			if err := viper.ReadInConfig(); err != nil {
				t.Fatal(err)
			}
			blocked := filepath.Join(home, "not-a-directory")
			if fail {
				writeCommandFile(t, blocked, "owned obstruction")
				viper.SetConfigFile(filepath.Join(blocked, ".warden.yaml"))
			}
			server := t.TempDir()
			output, err := runOutcomeCommand(t, command.NewConfigCommand(config.Config{}), "set", "valheim-directory", server)
			saved := viper.New()
			saved.SetConfigFile(path)
			if readErr := saved.ReadInConfig(); readErr != nil {
				t.Fatal(readErr)
			}
			if fail {
				var pathError *os.PathError
				if !errors.As(err, &pathError) || pathError.Path != filepath.Join(blocked, ".warden.yaml") ||
					strings.Count(output, "unable to save configuration") != 1 || strings.Count(output, "Error:") != 1 ||
					strings.Contains(output, "Usage:") || saved.GetString("valheim-directory") != "original" {
					t.Errorf("failed persistence was hidden: error=%v, output=%q, saved=%v", err, output, saved.AllSettings())
				}
			} else if err != nil || output != "" || saved.GetString("valheim-directory") != server {
				t.Errorf("successful persistence changed: error=%v, output=%q, saved=%v", err, output, saved.AllSettings())
			}
		})
	}
}

func TestExecuteReusedCommand(t *testing.T) {
	cause := errors.New("injected list failure")
	var result error = cause
	calls := 0
	cmd := command.NewListCommand(&listCommandStub{list: func() ([]mod.Mod, error) {
		calls++
		return nil, result
	}})
	for _, tt := range []struct {
		name       string
		args       []string
		result     error
		wantErr    bool
		calls      int
		usage      int
		diagnostic string
	}{
		{name: "runtime failure", result: cause, wantErr: true, calls: 1, diagnostic: cause.Error()},
		{name: "invalid flag after failure", args: []string{"--invalid"}, wantErr: true, calls: 1, usage: 1, diagnostic: "unknown flag"},
		{name: "success after failure", calls: 2, diagnostic: "no mods are installed"},
		{name: "help after failure", args: []string{"--help"}, calls: 2, usage: 1, diagnostic: "List all currently installed mods"},
	} {
		t.Run("given a reused command, when executing "+tt.name+", then use only the current outcome", func(t *testing.T) {
			result = tt.result
			output, err := runOutcomeCommand(t, cmd, tt.args...)
			if (err != nil) != tt.wantErr || (tt.result != nil && !errors.Is(err, tt.result)) ||
				calls != tt.calls || strings.Count(output, "Usage:") != tt.usage ||
				strings.Count(output, tt.diagnostic) != 1 {
				t.Errorf("prior invocation leaked into execution: calls=%d, error=%v, output=%q", calls, err, output)
			}
		})
	}
}

func TestExecuteCommandIsolation(t *testing.T) {
	calls := 0
	cmd := command.NewListCommand(&listCommandStub{list: func() ([]mod.Mod, error) {
		calls++
		return nil, nil
	}})
	if _, err := runRootCommand(t, []string{"list"}, cmd); err != nil {
		t.Fatal(err)
	}
	output, err := runRootCommand(t, []string{"list"}, command.NewUpdateCommand(nil, nil))
	if err == nil || calls != 1 || strings.Count(output, `unknown command "list"`) != 1 ||
		strings.Count(output, "Error:") != 1 || strings.Count(output, "Usage:") != 1 {
		t.Errorf("a previous root's commands leaked into execution: calls=%d, error=%v, output=%q", calls, err, output)
	}
}

func runtimeCommand(t *testing.T, operation string, result func() error) (*cobra.Command, []string) {
	t.Helper()
	identity := func(namespace, name string) error {
		if namespace != "Owner" || name != "Root" {
			t.Fatalf("unexpected package %s/%s", namespace, name)
		}
		return result()
	}
	switch operation {
	case "add":
		fs := &standaloneFrameworkStub{install: func() error { return service.ErrNoChanges }}
		return command.NewAddCommand(fs, &completionModStub{add: identity}), []string{"--namespace", "Owner", "--mod", "Root"}
	case "bootstrap":
		fs := &standaloneFrameworkStub{install: result}
		ms := &completionModStub{add: func(string, string) error {
			t.Fatal("incomplete bootstrap allowed mod work")
			return nil
		}}
		return command.NewAddCommand(fs, ms), []string{"--namespace", "Owner", "--mod", "Root"}
	case "update mod":
		ms := &identityUpdateStub{update: func(name string) error { return identity("Owner", name) }}
		return command.NewUpdateCommand(nil, ms), []string{"--mod", "Root"}
	case "update qualified":
		ms := &identityUpdateStub{updateIdentity: identity}
		return command.NewUpdateCommand(nil, ms), []string{"--namespace", "Owner", "--mod", "Root"}
	case "update all":
		return command.NewUpdateCommand(nil, &updateModStub{updateAll: result}), []string{"all"}
	case "update framework":
		return command.NewUpdateCommand(&updateFrameworkStub{update: result}, nil), []string{"bepinex"}
	case "remove mod":
		return command.NewRemoveCommand(nil, &removalCommandStub{remove: identity}), []string{"--namespace", "Owner", "--mod", "Root"}
	case "remove all":
		return command.NewRemoveCommand(nil, &removalCommandStub{all: result}), []string{"all"}
	case "remove framework":
		return command.NewRemoveCommand(&standaloneFrameworkStub{remove: result}, nil), []string{"bepinex"}
	case "list":
		ms := &listCommandStub{list: func() ([]mod.Mod, error) {
			return []mod.Mod{{Name: "untrusted partial row"}}, result()
		}}
		return command.NewListCommand(ms), nil
	case "start":
		server := &serverCommandStub{
			valid: func(gameType string) bool { return gameType == "modded" },
			start: func(gameType string) (string, error) {
				if gameType != "modded" {
					t.Fatalf("unexpected server type %q", gameType)
				}
				return "", result()
			},
		}
		return command.NewStartCommand(server), []string{"modded"}
	default:
		t.Fatalf("unrecognized test operation %q", operation)
		return nil, nil
	}
}

type listCommandStub struct {
	service.Mod
	list func() ([]mod.Mod, error)
}

func (s *listCommandStub) ListMods() ([]mod.Mod, error) { return s.list() }

type removalCommandStub struct {
	service.Mod
	remove func(string, string) error
	all    func() error
}

func (s *removalCommandStub) RemoveMod(namespace, name string) error {
	return s.remove(namespace, name)
}
func (s *removalCommandStub) RemoveAllMods() error { return s.all() }

type serverCommandStub struct {
	valid func(string) bool
	start func(string) (string, error)
}

func (s *serverCommandStub) IsValidGameType(gameType string) bool { return s.valid(gameType) }
func (s *serverCommandStub) Start(gameType string) (string, error) {
	return s.start(gameType)
}
