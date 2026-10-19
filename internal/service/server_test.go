package service_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/config"
	"warden/internal/service"
)

func TestStart_Happy(t *testing.T) {
	tests := map[string]struct {
		gameType string
		expected string
	}{
		"successfully starts vanilla game server": {
			gameType: "vanilla",
			expected: "Starting Vanilla Server",
		},
		"successfully starts modded game server": {
			gameType: "modded",
			expected: "Starting Modded Server",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := config.Config{
				ValheimDirectory: "../test/data",
				Platform:         config.Linux,
			}
			ss := service.NewServerService(cfg)

			output, err := ss.Start(tt.gameType)
			if err != nil {
				t.Errorf("unexpected error, received: %+v", err)
			}
			if strings.TrimSpace(output) != tt.expected {
				t.Errorf("expected output: %s, received: %s", tt.expected, output)
			}
		})
	}
}

func TestStart_Sad(t *testing.T) {
	ss := service.NewServerService(config.Config{})
	output, err := ss.Start("niaudbiwabdiu dd")
	if !errors.Is(err, service.ErrInvalidGameType) {
		t.Errorf("expected error: %+v, received: %+v", service.ErrInvalidGameType, err)
	}
	if errors.Is(err, service.ErrServerStartFailed) || output != "" {
		t.Errorf("invalid game type attempted startup: output=%q, error=%v", output, err)
	}
}

func TestStart_FromConfiguredDirectory(t *testing.T) {
	t.Setenv("WARDEN_SERVER_FIXTURE", "inherited environment")

	tests := map[string]struct {
		gameType string
		platform string
		script   string
	}{
		"given a Linux vanilla server": {
			gameType: " VaNiLlA ",
			platform: config.Linux,
			script:   "start_server.sh",
		},
		"given a macOS vanilla script selection": {
			gameType: "vanilla",
			platform: config.MacOS,
			script:   "start_server.command",
		},
		"given a Windows vanilla script selection": {
			gameType: "vanilla",
			platform: config.Windows,
			script:   "start_headless_server.bat",
		},
		"given a modded server": {
			gameType: " MoDdEd ",
			platform: config.Linux,
			script:   "start_server_bepinex.sh",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			callerDirectory := t.TempDir()
			serverDirectory := filepath.Join(callerDirectory, "server directory")
			if err := os.Mkdir(serverDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{
				tt.script:              "#!/bin/sh\nset -eu\nprintf 'script arguments: %s\\n' \"$#\"\nexec ./server-fixture.sh\n",
				"server-fixture.sh":    "#!/bin/sh\nset -eu\npwd -P\ncat ./fixture-resource.txt\nprintf '%s\\n' \"$WARDEN_SERVER_FIXTURE\"\nprintf 'fixture stderr\\n' >&2\nprintf 'finished\\n'\n",
				"fixture-resource.txt": "relative resource\n",
			} {
				if err := os.WriteFile(filepath.Join(serverDirectory, name), []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			physicalDirectory, err := filepath.EvalSymlinks(serverDirectory)
			if err != nil {
				t.Fatal(err)
			}
			expected := "script arguments: 0\n" + physicalDirectory + "\nrelative resource\ninherited environment\nfixture stderr\nfinished\n"

			control := exec.Command("sh", filepath.Join(serverDirectory, tt.script))
			control.Dir = serverDirectory
			if output, err := control.CombinedOutput(); err != nil || string(output) != expected {
				t.Fatalf("same-script server-directory control: output=%q, error=%v", output, err)
			}
			t.Log("same-script server-directory control passed")

			updatePreflightWorkingDirectory(t, callerDirectory)
			originalDirectory, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			for name, directory := range map[string]string{
				"when configured absolutely, then start from the server directory": serverDirectory,
				"when configured relatively, then start from the server directory": "server directory",
			} {
				t.Run(name, func(t *testing.T) {
					ss := service.NewServerService(config.Config{
						ValheimDirectory: directory,
						Platform:         tt.platform,
					})
					output, err := ss.Start(tt.gameType)
					if err != nil {
						t.Fatalf("start from unrelated caller directory: output=%q, error=%v", output, err)
					}
					if output != expected {
						t.Errorf("expected output %q, received %q", expected, output)
					}
					if currentDirectory, err := os.Getwd(); err != nil || currentDirectory != originalDirectory {
						t.Errorf("caller directory changed: directory=%q, error=%v", currentDirectory, err)
					}
				})
			}
		})
	}
}

func TestStart_Failure(t *testing.T) {
	tests := map[string]struct {
		script           string
		missingDirectory bool
		exitCode         int
	}{
		"given a missing script, when starting, then return a startup error with shell diagnostics": {},
		"given a failing script, when starting, then preserve output and the exit cause": {
			script:   "printf 'fixture stdout\\n'\nprintf 'fixture stderr\\n' >&2\nexit 23\n",
			exitCode: 23,
		},
		"given a missing server directory, when starting, then preserve the filesystem cause": {
			missingDirectory: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if tt.missingDirectory {
				directory = filepath.Join(directory, "missing")
			}
			if tt.script != "" {
				if err := os.WriteFile(filepath.Join(directory, "start_server.sh"), []byte(tt.script), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ss := service.NewServerService(config.Config{
				ValheimDirectory: directory,
				Platform:         config.Linux,
			})
			output, err := ss.Start("vanilla")
			if !errors.Is(err, service.ErrServerStartFailed) || errors.Is(err, service.ErrInvalidGameType) {
				t.Fatalf("expected startup failure, received %v", err)
			}
			if tt.missingDirectory {
				if !errors.Is(err, os.ErrNotExist) || output != "" {
					t.Errorf("expected missing-directory cause without output, received %q, %v", output, err)
				}
				return
			}
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) {
				t.Fatalf("missing process exit cause: %v", err)
			}
			if tt.script == "" {
				if exitError.ExitCode() == 0 || !strings.Contains(output, "start_server.sh") {
					t.Errorf("missing-script failure lost shell diagnostics: output=%q, error=%v", output, err)
				}
			} else if exitError.ExitCode() != tt.exitCode || output != "fixture stdout\nfixture stderr\n" {
				t.Errorf("script failure lost output or exit status: output=%q, error=%v", output, err)
			}
		})
	}
}

func TestIsValidGameType_Happy(t *testing.T) {
	tests := map[string]struct {
		config string
	}{
		"return true if game config is vanilla": {
			config: "vanilla",
		},
		"return true if game config is modded": {
			config: "modded",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ss := service.NewServerService(config.Config{})
			if !ss.IsValidGameType(tt.config) {
				t.Error("expected true, got false")
			}
		})
	}
}

func TestIsValidGameType_Sad(t *testing.T) {
	ss := service.NewServerService(config.Config{})

	if ss.IsValidGameType("RANDOM TEST VALUE") {
		t.Error("expected false, got true")
	}
}
