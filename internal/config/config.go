package config

import (
	"errors"
	"fmt"
	"os/user"
	"path/filepath"
	"runtime"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
)

const (
	configName = ".warden"
	configType = "yaml"

	// These values are based on what the Go stdlib labels each operating system
	MacOS   = "darwin"
	Windows = "windows"
	Linux   = "linux"

	WardenConfigFile               = configName + "." + configType
	DefaultSteamLinuxInstallPath   = "~/.steam/SteamApps/common/Valheim dedicated server"
	DefaultSteamMacOSInstallPath   = "/Library/Application Support/Steam/steamapps/common/Valheim dedicated server"
	DefaultSteamWindowsInstallPath = "C:\\Program Files (x86)\\Steam\\steamapps\\common\\Valheim Dedicated Server"
)

var (
	ErrUnableToWriteConfig = errors.New("unable to write to config file")
	ErrFailedToReadConfig  = errors.New("unable to read config from YAML file")
)

// Config represents all configurable values needed to make warden work, e.g. the directory
// to install mods to
type Config struct {
	// The directory containing all of the Valheim server files
	ValheimDirectory string `mapstructure:"valheim-directory"`

	// The type of operating system the server is running on, e.g. Windows, Linux, or macOS
	Platform string `mapstructure:"platform"`
}

// Load creates a new instance of Config, based on a configuration YAML file at the given
// path. If one doesn't exist, a new file is created with default values
func Load(path string) (*Config, error) {
	return load(path, homedir.Expand)
}

func load(path string, expandHome func(string) (string, error)) (*Config, error) {
	viper.AddConfigPath(path)
	viper.SetConfigName(configName)
	viper.SetConfigType(configType)

	err := viper.ReadInConfig()
	missingConfig := errors.As(err, &viper.ConfigFileNotFoundError{})
	if err != nil && !missingConfig {
		return nil, ErrFailedToReadConfig
	}

	viper.SetDefault("platform", runtime.GOOS)
	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, ErrFailedToReadConfig
	}

	if !viper.IsSet("valheim-directory") {
		directory := GetInstallPath(cfg.Platform)
		if cfg.Platform == Linux {
			directory, err = expandHome(directory)
			if err != nil {
				return nil, fmt.Errorf("%w: resolve Linux default valheim-directory: %w", ErrFailedToReadConfig, err)
			}
			if !filepath.IsAbs(directory) {
				return nil, fmt.Errorf("%w: Linux default valheim-directory %q is not absolute", ErrFailedToReadConfig, directory)
			}
		}
		cfg.ValheimDirectory = directory
		viper.SetDefault("valheim-directory", directory)
	}

	if missingConfig {
		if err := createConfigFile(cfg, path); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// Creates a new configuration file called .warden.yaml, with the given settings
func createConfigFile(cfg *Config, path string) error {
	viper.Set("valheim-directory", cfg.ValheimDirectory)
	viper.Set("platform", cfg.Platform)

	file := filepath.Join(path, WardenConfigFile)
	if err := viper.WriteConfigAs(file); err != nil {
		return fmt.Errorf("%w: %w", ErrUnableToWriteConfig, err)
	}
	return nil
}

// GetInstallPath returns the platform's default path template. Load resolves the Linux home prefix.
func GetInstallPath(os string) string {
	if os == Windows {
		return DefaultSteamWindowsInstallPath
	}
	if os == Linux {
		return DefaultSteamLinuxInstallPath
	}
	if os == MacOS {
		// Get user
		currentUser, err := user.Current()
		if err != nil {
			panic("unable to fetch current user")
		}
		return filepath.Join("/Users", currentUser.Username, DefaultSteamMacOSInstallPath)
	}
	return "" // figure out how to handle a default value
}
