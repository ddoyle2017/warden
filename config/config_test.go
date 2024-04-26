package config_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/config"
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
				ValheimDirectory: filepath.Join(testConfigPath, config.DefaultSteamInstallPath),
				ModDirectory:     config.DefaultModInstallPath,
			},
		},
		"if config file does exist, load existing values and return success": {
			setUp: func() error {
				return createTestConfigFile("mod-directory: /test/file\nvalheim-directory: .\n")
			},
			expected: config.Config{
				ValheimDirectory: ".",
				ModDirectory:     "/test/file",
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
			if cfg.ValheimDirectory != test.expected.ValheimDirectory {
				t.Errorf("expected config: %s, received: %s", test.expected.ValheimDirectory, cfg.ValheimDirectory)
			}
			if cfg.ModDirectory != test.expected.ModDirectory {
				t.Errorf("expected config: %s, received: %s", test.expected.ModDirectory, cfg.ModDirectory)
			}

			t.Cleanup(func() {
				path := filepath.Join(testConfigPath, config.WardenConfigFile)

				if err := os.RemoveAll(path); err != nil {
					t.Errorf("unable to clean-up test config file, error: %+v", err)
				}
			})
		})
	}
}

func TestLoad_Sad(t *testing.T) {
	tests := map[string]struct {
		setUp    func() error
		expected error
	}{
		"if config file is malformed, return an error": {
			setUp: func() error {
				return createTestConfigFile("mod-directorydwqd /test/file\nvalh       eim-directory{} .\n")
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
		})
	}
}

func createTestConfigFile(content string) error {
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
