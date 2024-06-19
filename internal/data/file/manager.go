package file

import (
	"errors"
	"path/filepath"
	"warden/internal/api"
)

const (
	// BepInEx is required by practically every mod for Valheim, so we use it
	// for the default path
	BepInExPluginDirectory = "/BepInEx/plugins"

	// A sub-directory containing the files and libraries needed for BepInEx to work
	BepInExContentsDirectory = "/BepInExPack_Valheim"
)

var (
	ErrZipDeleteFailed        = errors.New("unable to delete zip archive")
	ErrModDeleteFailed        = errors.New("unable to delete mod directory")
	ErrDeleteAllModsFailed    = errors.New("unable to delete all mods")
	ErrFrameworkInstallFailed = errors.New("unable to install framework")
	ErrFrameworkDeleteFailed  = errors.New("unable to delete framework")
	ErrFrameworkUpdateFailed  = errors.New("unable to update framework")
)

// Manager provides an interface for all file-related mod operations, e.g. installing and deleting mods.
type Manager interface {
	modManager
	frameworkManager
}

type manager struct {
	backup           Backup
	client           api.HTTPClient
	valheimDirectory string
	modDirectory     string
}

func NewManager(c api.HTTPClient, vd string) Manager {
	return &manager{
		backup:           NewBackup(),
		client:           c,
		valheimDirectory: vd,
		modDirectory:     filepath.Join(vd, BepInExPluginDirectory),
	}
}
