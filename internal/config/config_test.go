package config_test

import (
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/config"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
)

func TestLoad(t *testing.T) {
	absoluteDirectory := filepath.Join(t.TempDir(), "configured server")
	tests := map[string]struct {
		content           string
		expected          config.Config
		expectedPersisted *config.Config
		expectedErr       error
		relativeHome      bool
	}{
		"given no config file, when loading, then persist and return defaults": {
			expected: config.Config{
				ValheimDirectory: config.GetInstallPath(runtime.GOOS),
				Platform:         runtime.GOOS,
			},
		},
		"given an existing config, when loading, then preserve saved values": {
			content: "# Local settings\nvalheim-directory: './server'\nplatform: linux\n",
			expected: config.Config{
				ValheimDirectory: "./server",
				Platform:         config.Linux,
			},
		},
		"given a Windows platform without a directory, when loading, then use the configured platform default": {
			content: "platform: windows\n",
			expected: config.Config{
				ValheimDirectory: config.DefaultSteamWindowsInstallPath,
				Platform:         config.Windows,
			},
			expectedPersisted: &config.Config{Platform: config.Windows},
		},
		"given a directory without a platform, when loading, then default to the host platform": {
			content: "valheim-directory: ./server\n",
			expected: config.Config{
				ValheimDirectory: "./server",
				Platform:         runtime.GOOS,
			},
			expectedPersisted: &config.Config{ValheimDirectory: "./server"},
		},
		"given an absolute server path, when loading, then preserve the path": {
			content: fmt.Sprintf("valheim-directory: %q\nplatform: linux\n", absoluteDirectory),
			expected: config.Config{
				ValheimDirectory: absoluteDirectory,
				Platform:         config.Linux,
			},
		},
		"given an explicit home-relative path, when loading, then preserve it without expansion": {
			content: "valheim-directory: ~/custom server\nplatform: linux\n",
			expected: config.Config{
				ValheimDirectory: "~/custom server",
				Platform:         config.Linux,
			},
		},
		"given a saved literal Linux default, when loading, then preserve the configured value": {
			content: "valheim-directory: ~/.steam/SteamApps/common/Valheim dedicated server\nplatform: linux\n",
			expected: config.Config{
				ValheimDirectory: "~/.steam/SteamApps/common/Valheim dedicated server",
				Platform:         config.Linux,
			},
		},
		"given an environment-variable path, when loading, then preserve it without expansion": {
			content: "valheim-directory: $HOME/custom server\nplatform: linux\n",
			expected: config.Config{
				ValheimDirectory: "$HOME/custom server",
				Platform:         config.Linux,
			},
		},
		"given an explicitly empty path, when loading, then preserve the configured value": {
			content: "valheim-directory: \"\"\nplatform: linux\n",
			expected: config.Config{
				Platform: config.Linux,
			},
		},
		"given an unknown platform with a relative directory, when loading, then preserve both values": {
			content: "platform: linuz\nvalheim-directory: ./server\n",
			expected: config.Config{
				ValheimDirectory: "./server",
				Platform:         "linuz",
			},
		},
		"given an unknown platform with an empty directory, when loading, then preserve both values": {
			content: "platform: linuz\nvalheim-directory: \"\"\n",
			expected: config.Config{
				Platform: "linuz",
			},
		},
		"given an empty platform with an absolute directory, when loading, then preserve both values": {
			content: fmt.Sprintf("platform: \"\"\nvalheim-directory: %q\n", absoluteDirectory),
			expected: config.Config{
				ValheimDirectory: absoluteDirectory,
			},
		},
		"given an empty platform with an empty directory, when loading, then preserve both values": {
			content: "platform: \"\"\nvalheim-directory: \"\"\n",
		},
		"given a relative home and Linux platform, when loading, then reject the default without publishing it": {
			content:      "platform: linux\n",
			expectedErr:  config.ErrFailedToReadConfig,
			relativeHome: true,
		},
		"given malformed YAML, when loading, then return a read error": {
			content:     "valheim-directory: [\n",
			expectedErr: config.ErrFailedToReadConfig,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			homedir.Reset()
			t.Cleanup(homedir.Reset)
			home := t.TempDir()
			if test.relativeHome {
				home = "relative-home"
			}
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			directory := t.TempDir()
			path := filepath.Join(directory, config.WardenConfigFile)
			expected := test.expected
			if test.content == "" && runtime.GOOS == config.Linux {
				expected.ValheimDirectory = filepath.Join(home, ".steam", "SteamApps", "common", "Valheim dedicated server")
			}
			if test.content != "" {
				if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			cfg, err := config.Load(directory)
			if !errors.Is(err, test.expectedErr) {
				t.Fatalf("expected error %v, received %v", test.expectedErr, err)
			}
			if test.content != "" {
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(content) != test.content {
					t.Errorf("existing configuration was rewritten: expected %q, received %q", test.content, content)
				}
			}
			if test.expectedErr != nil {
				if cfg != nil {
					t.Errorf("expected nil config, received %+v", cfg)
				}
				if viper.IsSet("valheim-directory") {
					t.Errorf("failed loading published a directory: %q", viper.GetString("valheim-directory"))
				}
				return
			}
			if cfg == nil {
				t.Fatal("expected a config, received nil")
			}
			if *cfg != expected {
				t.Errorf("expected config %+v, received %+v", expected, *cfg)
			}
			saved := viper.New()
			saved.SetConfigFile(path)
			if err := saved.ReadInConfig(); err != nil {
				t.Fatal(err)
			}
			var persisted config.Config
			if err := saved.Unmarshal(&persisted); err != nil {
				t.Fatal(err)
			}
			expectedPersisted := expected
			if test.expectedPersisted != nil {
				expectedPersisted = *test.expectedPersisted
			}
			if persisted != expectedPersisted {
				t.Errorf("expected persisted config %+v, received %+v", expectedPersisted, persisted)
			}
			if directory := viper.GetString("valheim-directory"); directory != expected.ValheimDirectory {
				t.Errorf("expected visible directory %q, received %q", expected.ValheimDirectory, directory)
			}
			if platform := viper.GetString("platform"); platform != expected.Platform {
				t.Errorf("expected visible platform %q, received %q", expected.Platform, platform)
			}
		})
	}
}

func TestLoadInvalidPlatformDefault(t *testing.T) {
	tests := map[string]string{
		"given an unknown platform without a directory, when loading, then require manual configuration repair": "linuz",
		"given an empty platform without a directory, when loading, then require manual configuration repair":   "",
	}
	for name, platform := range tests {
		t.Run(name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			homedir.Reset()
			t.Cleanup(homedir.Reset)
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			directory := t.TempDir()
			path := filepath.Join(directory, config.WardenConfigFile)
			content := fmt.Sprintf("platform: %q\n", platform)
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}

			cfg, err := config.Load(directory)
			if !errors.Is(err, config.ErrFailedToReadConfig) {
				t.Fatalf("expected configuration error, received %v", err)
			}
			if cfg != nil {
				t.Errorf("expected nil config, received %+v", cfg)
			}
			for _, detail := range []string{fmt.Sprintf("platform %q", platform), "edit", path, "valheim-directory"} {
				if !strings.Contains(err.Error(), detail) {
					t.Errorf("expected manual-repair detail %q, received %v", detail, err)
				}
			}
			if viper.IsSet("valheim-directory") {
				t.Errorf("invalid platform published a directory: %q", viper.GetString("valheim-directory"))
			}
			saved, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(saved) != content {
				t.Errorf("expected unchanged configuration %q, received %q", content, saved)
			}
		})
	}
}

func TestLoadLinuxDefault(t *testing.T) {
	tests := map[string]string{
		"given no config and Linux selected, when loading, then persist the resolved home default": "",
		"given a Linux config without a directory, when loading, then resolve the home default":    "platform: linux\n",
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			homedir.Reset()
			t.Cleanup(homedir.Reset)
			home := filepath.Join(t.TempDir(), "home with spaces")
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			server := filepath.Join(home, ".steam", "SteamApps", "common", "Valheim dedicated server")
			if err := os.MkdirAll(server, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(server, "server.txt"), []byte("owned server fixture"), 0o600); err != nil {
				t.Fatal(err)
			}

			directory := t.TempDir()
			path := filepath.Join(directory, config.WardenConfigFile)
			if content == "" {
				viper.Set("platform", config.Linux)
			} else if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}

			expected := config.Config{ValheimDirectory: server, Platform: config.Linux}
			cfg, err := config.Load(directory)
			if err != nil {
				t.Fatal(err)
			}
			if cfg == nil || *cfg != expected {
				t.Fatalf("expected config %+v, received %+v", expected, cfg)
			}
			if !filepath.IsAbs(cfg.ValheimDirectory) {
				t.Errorf("expected an absolute default, received %q", cfg.ValheimDirectory)
			}
			fixture, err := os.ReadFile(filepath.Join(cfg.ValheimDirectory, "server.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(fixture) != "owned server fixture" {
				t.Errorf("expected owned server fixture, received %q", fixture)
			}
			if visible := viper.GetString("valheim-directory"); visible != server {
				t.Errorf("expected visible directory %q, received %q", server, visible)
			}

			if content == "" {
				saved := viper.New()
				saved.SetConfigFile(path)
				if err := saved.ReadInConfig(); err != nil {
					t.Fatal(err)
				}
				var persisted config.Config
				if err := saved.Unmarshal(&persisted); err != nil {
					t.Fatal(err)
				}
				if persisted != expected {
					t.Errorf("expected persisted config %+v, received %+v", expected, persisted)
				}
			} else {
				saved, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(saved) != content {
					t.Errorf("expected unchanged config %q, received %q", content, saved)
				}
			}

			viper.Reset()
			reloaded, err := config.Load(directory)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded == nil || *reloaded != expected {
				t.Errorf("expected reloaded config %+v, received %+v", expected, reloaded)
			}
		})
	}
}

func TestLoadWriteFailure(t *testing.T) {
	t.Run("given a missing configuration directory, when loading, then return the write failure without creating files", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
		homedir.Reset()
		t.Cleanup(homedir.Reset)
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		viper.Set("platform", config.Linux)
		directory := filepath.Join(t.TempDir(), "missing")
		path := filepath.Join(directory, config.WardenConfigFile)

		cfg, err := config.Load(directory)
		if cfg != nil {
			t.Errorf("expected nil config, received %+v", cfg)
		}
		if !errors.Is(err, config.ErrUnableToWriteConfig) {
			t.Errorf("expected configuration write error, received %v", err)
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected missing-directory cause, received %v", err)
		}
		var pathErr *os.PathError
		if !errors.As(err, &pathErr) {
			t.Errorf("expected filesystem cause, received %v", err)
		} else if pathErr.Path != path {
			t.Errorf("expected failed destination %q, received %q", path, pathErr.Path)
		}

		for _, missing := range []string{directory, path} {
			if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("expected %q to remain absent, received %v", missing, err)
			}
		}
	})
}
