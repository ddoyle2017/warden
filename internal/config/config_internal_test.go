package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadHomeResolutionFailure(t *testing.T) {
	cause := errors.New("home lookup failed")
	tests := map[string]struct {
		content string
		result  string
		err     error
	}{
		"given no config, when home lookup fails, then return the cause without creating a config": {
			err: cause,
		},
		"given a Linux config without a directory, when home lookup fails, then return the cause without changing the file": {
			content: "platform: linux\n",
			err:     cause,
		},
		"given a relative home, when resolving the default, then reject the relative result": {
			result: "relative-home/.steam/SteamApps/common/Valheim dedicated server",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			viper.Set("platform", Linux)
			directory := t.TempDir()
			path := filepath.Join(directory, WardenConfigFile)
			if test.content != "" {
				if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			cfg, err := load(directory, func(path string) (string, error) {
				calls++
				if path != "~/.steam/SteamApps/common/Valheim dedicated server" {
					t.Errorf("unexpected default to expand: %q", path)
				}
				return test.result, test.err
			})
			if !errors.Is(err, ErrFailedToReadConfig) {
				t.Errorf("expected a config read error, received %v", err)
			}
			if test.err != nil && !errors.Is(err, test.err) {
				t.Errorf("expected home lookup cause %v, received %v", test.err, err)
			}
			if cfg != nil {
				t.Errorf("expected nil config, received %+v", cfg)
			}
			if calls != 1 {
				t.Errorf("expected one home expansion, received %d", calls)
			}
			if viper.IsSet("valheim-directory") {
				t.Errorf("failed resolution published a directory: %q", viper.GetString("valheim-directory"))
			}
			if test.content == "" {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("expected no config file, received %v", err)
				}
			} else {
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(content) != test.content {
					t.Errorf("expected unchanged config %q, received %q", test.content, content)
				}
			}
		})
	}
}

func TestLoadWithoutHomeResolution(t *testing.T) {
	tests := map[string]struct {
		content          string
		expected         Config
		platformOverride string
	}{
		"given an explicit directory, when loading, then skip home resolution": {
			content:  "platform: linux\nvalheim-directory: ./configured server\n",
			expected: Config{ValheimDirectory: "./configured server", Platform: Linux},
		},
		"given a Windows default, when loading, then skip home resolution": {
			content:  "platform: windows\n",
			expected: Config{ValheimDirectory: DefaultSteamWindowsInstallPath, Platform: Windows},
		},
		"given an unknown platform outside the file, when loading, then preserve the existing default behavior": {
			content:          "{}\n",
			expected:         Config{Platform: "unsupported"},
			platformOverride: "unsupported",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			if test.platformOverride != "" {
				viper.Set("platform", test.platformOverride)
			}
			directory := t.TempDir()
			path := filepath.Join(directory, WardenConfigFile)
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}

			cfg, err := load(directory, func(string) (string, error) {
				t.Fatal("this configuration must not require home resolution")
				return "", nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if cfg == nil || *cfg != test.expected {
				t.Errorf("expected config %+v, received %+v", test.expected, cfg)
			}
			if visible := viper.GetString("valheim-directory"); visible != test.expected.ValheimDirectory {
				t.Errorf("expected visible directory %q, received %q", test.expected.ValheimDirectory, visible)
			}
			saved, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(saved) != test.content {
				t.Errorf("expected unchanged configuration %q, received %q", test.content, saved)
			}
		})
	}
}
