package config_test

import (
	"errors"
	"github.com/ddoyle2017/warden/internal/config"
	"os"
	"path/filepath"
	"runtime"
	"testing"

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
			content: "valheim-directory: ./server\nplatform: linux\n",
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
