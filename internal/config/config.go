package config

import (
	"errors"
	"path/filepath"

	"github.com/spf13/viper"
)

const (
	WardenConfigFile        = ".warden.yaml"
	DefaultSteamInstallPath = ".steam/SteamApps/common/Valheim dedicated server"
)

var (
	ErrUnableToWriteConfig = errors.New("unable to write to config file")
	ErrFailedToReadConfig  = errors.New("unable to read config from YAML file")
)

// Config represents all configurable values needed to make warden work, e.g. the directory
// to install mods to
type Config struct {
	ValheimDirectory string `mapstructure:"valheim-directory"`
}

// Load creates a new instance of Config, based on a configuration YAML file at the given
// path. If one doesn't exist, a new file is created with default values
func Load(path string) (*Config, error) {
	viper.AddConfigPath(path)
	viper.SetConfigName(".warden")
	viper.SetConfigType("yaml")

	err := viper.ReadInConfig()
	cfg := &Config{
		ValheimDirectory: DefaultSteamInstallPath,
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

	file := filepath.Join(path, WardenConfigFile)
	if err := viper.WriteConfigAs(file); err != nil {
		return ErrUnableToWriteConfig
	}
	return nil
}