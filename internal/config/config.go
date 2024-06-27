package config

import (
	"errors"
	"path/filepath"
	"runtime"

	"github.com/spf13/viper"
)

const (
	configName = ".warden"
	configType = "yaml"

	// These values are based on what the Go stdlib labels each operating system
	MacOS   = "darwin"
	Windows = "windows"
	Linux   = "linux"

	WardenConfigFile        = configName + "." + configType
	DefaultSteamInstallPath = ".steam/SteamApps/common/Valheim dedicated server"
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
	viper.AddConfigPath(path)
	viper.SetConfigName(configName)
	viper.SetConfigType(configType)

	err := viper.ReadInConfig()
	cfg := &Config{
		ValheimDirectory: DefaultSteamInstallPath,
		Platform:         runtime.GOOS,
	}

	// If config doesn't exist, create the file and add default values
	if errors.As(err, &viper.ConfigFileNotFoundError{}) {
		createConfigFile(cfg, path)
	} else if err != nil {
		return nil, ErrFailedToReadConfig
	}

	// Load in settings from YAML file into Config struct
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, ErrFailedToReadConfig
	}
	return cfg, nil
}

// Creates a new configuration file called .warden.yaml, with the given settings
func createConfigFile(cfg *Config, path string) error {
	viper.Set("valheim-directory", cfg.ValheimDirectory)
	viper.Set("platform", cfg.Platform)

	file := filepath.Join(path, WardenConfigFile)
	if err := viper.WriteConfigAs(file); err != nil {
		return ErrUnableToWriteConfig
	}
	return nil
}
