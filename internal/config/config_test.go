package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"warden/internal/config"

	"github.com/spf13/viper"
)

func TestLoad(t *testing.T) {
	tests := map[string]struct {
		content     string
		expected    config.Config
		expectedErr error
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
		"given malformed YAML, when loading, then return a read error": {
			content:     "valheim-directory: [\n",
			expectedErr: config.ErrFailedToReadConfig,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			directory := t.TempDir()
			path := filepath.Join(directory, config.WardenConfigFile)
			if test.content != "" {
				if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			cfg, err := config.Load(directory)
			if !errors.Is(err, test.expectedErr) {
				t.Fatalf("expected error %v, received %v", test.expectedErr, err)
			}
			if test.expectedErr != nil {
				if cfg != nil {
					t.Errorf("expected nil config, received %+v", cfg)
				}
				return
			}
			if cfg == nil {
				t.Fatal("expected a config, received nil")
			}
			if *cfg != test.expected {
				t.Errorf("expected config %+v, received %+v", test.expected, *cfg)
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

			saved := viper.New()
			saved.SetConfigFile(path)
			if err := saved.ReadInConfig(); err != nil {
				t.Fatal(err)
			}
			var persisted config.Config
			if err := saved.Unmarshal(&persisted); err != nil {
				t.Fatal(err)
			}
			if persisted != test.expected {
				t.Errorf("expected persisted config %+v, received %+v", test.expected, persisted)
			}
		})
	}
}

func TestLoadWriteFailure(t *testing.T) {
	t.Run("given a missing configuration directory, when loading, then return the write failure without creating files", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
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
