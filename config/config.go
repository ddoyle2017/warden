package config

import (
	"errors"
	"log"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
)

const wardenConfigFile = ".warden.yaml"

var (
	ErrUnableToWriteConfig = errors.New("unable to write to config file")
	ErrFailedToReadConfig  = errors.New("unable to read config from YAML file")
)

// Config represents all configurable values needed to make warden work, e.g. the directory
// to install mods to
type Config struct {
	ModDirectory string `mapstructure:"mod-directory"`
}

type Option func(*Config)

func New(options ...Option) *Config {
	home, err := homedir.Dir()
	if err != nil {
		log.Fatal(err)
	}

	c := &Config{
		ModDirectory: home,
	}
	for _, opt := range options {
		opt(c)
	}
	return c
}

// The folder where all mods are installed
func WithModDirectory(directory string) Option {
	return func(c *Config) {
		c.ModDirectory = directory
	}
}

// LoadConfig looks for and loads in the .warden.yaml configuration file at
// the specified path
func (c *Config) LoadConfig(path string) error {
	viper.AddConfigPath(path)
	viper.SetConfigName(".warden")
	viper.SetConfigType("yaml")

	err := viper.ReadInConfig()
	if errors.As(err, &viper.ConfigFileNotFoundError{}) {
		createDefaultConfig(path)
	} else if err != nil {
		return ErrFailedToReadConfig
	}

	cfg := Config{}
	if err := viper.Unmarshal(&cfg); err != nil {
		return ErrFailedToReadConfig
	}
	return nil
}

// createDefaultConfig creates a new configuration file called .warden.yaml
func createDefaultConfig(path string) error {
	file := filepath.Join(path, wardenConfigFile)

	if err := viper.WriteConfigAs(file); err != nil {
		return ErrUnableToWriteConfig
	}
	return nil
}
