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

func TestLoadConfiguredPathWithoutHome(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	directory := t.TempDir()
	content := "platform: linux\nvalheim-directory: ./configured server\n"
	if err := os.WriteFile(filepath.Join(directory, WardenConfigFile), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := load(directory, func(string) (string, error) {
		t.Fatal("an explicit path must not require home resolution")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := Config{ValheimDirectory: "./configured server", Platform: Linux}
	if cfg == nil || *cfg != expected {
		t.Errorf("expected config %+v, received %+v", expected, cfg)
	}
}
