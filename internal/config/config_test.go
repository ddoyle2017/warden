package config_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"warden/internal/config"

	"github.com/spf13/viper"
)

const testConfigPath = "../test/config"

func TestLoad_Happy(t *testing.T) {
	tests := map[string]struct {
		setUp    func() error
		expected config.Config
	}{
		"if config file doesn't exist, create one with the default values and return success": {
			setUp: func() error {
				return nil
			},
			expected: config.Config{
				ValheimDirectory: config.DefaultSteamInstallPath,
				Platform:         runtime.GOOS,
			},
		},
		"if config file does exist, load existing values and return success": {
			setUp: func() error {
				return createTestConfigFile(t, "valheim-directory: ./test/file\nplatform: linux\n")
			},
			expected: config.Config{
				ValheimDirectory: "./test/file",
				Platform:         config.Linux,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.setUp()
			if err != nil {
				t.Errorf("unexpected error on test set-up, received error: %+v", err)
			}

			cfg, err := config.Load(testConfigPath)

			if err != nil {
				t.Errorf("expected a nil error, received: %+v", cfg)
			}
			if !doConfigsMatch(test.expected, *cfg) {
				t.Errorf("expected config: %+v, received: %+v", test.expected, cfg)
			}
			resetTestConfig(t)
		})
	}
	t.Cleanup(func() {
		resetTestConfig(t)
	})
}

func TestLoad_Sad(t *testing.T) {
	tests := map[string]struct {
		setUp    func() error
		expected error
	}{
		"if config file is malformed, return an error": {
			setUp: func() error {
				return createTestConfigFile(t, "valh       eim-directory{} .12341234\n")
			},
			expected: config.ErrFailedToReadConfig,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if err := test.setUp(); err != nil {
				t.Errorf("unexpected error on test set-up, received error: %+v", err)
			}

			cfg, err := config.Load(testConfigPath)

			if cfg != nil {
				t.Errorf("expected a nil Config, received: %+v", cfg)
			}
			if !errors.Is(err, test.expected) {
				t.Errorf("expected error: %+v, received: %+v", test.expected, err)
			}

			t.Cleanup(func() {
				resetTestConfig(t)
			})
		})
	}
}

func createTestConfigFile(t *testing.T, content string) error {
	resetTestConfig(t)

	path := filepath.Join(testConfigPath, config.WardenConfigFile)

	// Create the empty file
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()

	source := strings.NewReader(content)

	// Write the body to file
	_, err = io.Copy(out, source)
	if err != nil {
		return err
	}
	return nil
}

func resetTestConfig(t *testing.T) {
	viper.Reset()
	path := filepath.Join(testConfigPath, config.WardenConfigFile)

	if err := os.RemoveAll(path); err != nil {
		t.Errorf("unable to clean-up test config file, error: %+v", err)
	}
}

func doConfigsMatch(a, b config.Config) bool {
	if a.ValheimDirectory != b.ValheimDirectory {
		return false
	}
	if a.Platform != b.Platform {
		return false
	}
	return true
}
