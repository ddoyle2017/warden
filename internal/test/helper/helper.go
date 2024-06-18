package helper

import "testing"

const (
	ModFullName = "Azumatt-Where_You_At-1.0.9"
)

// Defines convenience functions for writing unit tests
type Helper interface {
	fileHelper
	databaseHelper
}

type helper struct {
	t             *testing.T
	dataFolder    string
	valheimFolder string
	databaseFile  string
	configFile    string
}

type Option func(h *helper)

func NewHelper(t *testing.T, opts ...Option) Helper {
	h := &helper{
		t:             t,
		dataFolder:    "../../test/data",
		valheimFolder: "../../test/file",
		databaseFile:  "warden-test.db",
		configFile:    "warden-test.yaml",
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

func WithDataFolder(path string) Option {
	return func(h *helper) {
		h.dataFolder = path
	}
}

func WithValheimFolder(path string) Option {
	return func(h *helper) {
		h.valheimFolder = path
	}
}

func WithDatabaseFile(filename string) Option {
	return func(h *helper) {
		h.databaseFile = filename
	}
}

func WithConfigFile(filename string) Option {
	return func(h *helper) {
		h.configFile = filename
	}
}
