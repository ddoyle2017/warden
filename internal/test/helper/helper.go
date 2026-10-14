package helper

import (
	"database/sql"
	"testing"
)

const (
	TestModFullName     = "Azumatt-Where_You_At-1.0.9"
	TestBepInExFullName = "denikson-BepInExPack_Valheim-5.4.2202"
	TestDownloadURL     = "testurl.com/file"
)

// Defines convenience functions for writing unit tests
type Helper interface {
	fileHelper
	databaseHelper

	GetValheimDirectory() string
	GetDataDirectory() string
}

type helper struct {
	t              *testing.T
	dataFolder     string
	valheimFolder  string
	databaseFolder string
	databaseFile   string
	configFile     string
	databases      []*sql.DB
}

type Option func(h *helper)

func NewHelper(t *testing.T, opts ...Option) Helper {
	t.Helper()

	h := &helper{
		t:              t,
		dataFolder:     "../../test/data",
		valheimFolder:  t.TempDir(),
		databaseFolder: t.TempDir(),
		databaseFile:   "warden-test.db",
		configFile:     "warden-test.yaml",
	}
	for _, o := range opts {
		o(h)
	}
	t.Cleanup(h.DeleteDatabase)
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

func (h *helper) GetValheimDirectory() string {
	return h.valheimFolder
}

func (h *helper) GetDataDirectory() string {
	return h.dataFolder
}
