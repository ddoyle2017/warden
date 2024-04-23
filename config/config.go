package config

import (
	"errors"
	"log"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
)

const (
	wardenConfigFile = ".warden.yaml"

	defaultSteamInstallPath = ".steam/SteamApps/common/Valheim dedicated server"

	// BepInEx is required by practically every mod for Valheim, so we use it
	// for the default path
	defaultModInstallPath = "/BepInEx/plugins"
)

var (
	ErrUnableToWriteConfig = errors.New("unable to write to config file")
	ErrFailedToReadConfig  = errors.New("unable to read config from YAML file")
)

// Config represents all configurable values needed to make warden work, e.g. the directory
// to install mods to
type Config struct {
	ValheimDirectory string `mapstructure:"valheim-directory"`
	ModDirectory     string `mapstructure:"mod-directory"`
}

// Load creates a new instance of Config, based on a configuration YAML file at the given
// path. If one doesn't exist, a new file is created with default values
func Load(path string) (*Config, error) {
	viper.AddConfigPath(path)
	viper.SetConfigName(".warden")
	viper.SetConfigType("yaml")

	err := viper.ReadInConfig()
	cfg := &Config{
		ValheimDirectory: getDefaultValheimDirectory(),
		ModDirectory:     defaultModInstallPath,
	}

	// If config doesn't exist, create the file and add default values
	if errors.As(err, &viper.ConfigFileNotFoundError{}) {
		createConfigFile(cfg, path)
	} else if err != nil {
		return nil, ErrFailedToReadConfig
	}

	// Load in settings from YAML file into Config struct
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, ErrFailedToReadConfig
	}
	return cfg, nil
}

// Returns the default installation path used by SteamCMD for Linux
func getDefaultValheimDirectory() string {
	home, err := homedir.Dir()
	if err != nil {
		log.Fatal(err)
	}
	return filepath.Join(home, defaultSteamInstallPath)
}

// Creates a new configuration file called .warden.yaml, with the given settings
func createConfigFile(cfg *Config, path string) error {
	viper.Set("valheim-directory", cfg.ValheimDirectory)
	viper.Set("mod-directory", cfg.ModDirectory)

	file := filepath.Join(path, wardenConfigFile)
	if err := viper.WriteConfigAs(file); err != nil {
		return ErrUnableToWriteConfig
	}
	return nil
}
