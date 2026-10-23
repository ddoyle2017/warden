package mock

import "github.com/ddoyle2017/warden/internal/data/file"

// Manager implements the file.Manager interface and exposes anonymous member functions for mocking
// file.Manager behavior
type Manager struct {
	ValidateModInstallFunc     func() error
	InstallModFunc             func(url, fullName string) (string, error)
	ReplaceModFunc             func(url, oldFullName, newFullName string) (file.ModReplacement, error)
	RemoveModFunc              func(fullName string) error
	RemoveAllModsFunc          func() error
	StageModRemovalFunc        func(fullName string) (file.ModRemoval, error)
	StageAllModsRemovalFunc    func() (file.ModRemoval, error)
	InstallBepInExFunc         func(url, fullName string) (string, error)
	UpdateBepInExFunc          func(url, fullName string) error
	RemoveBepInExFunc          func() error
	BepInExPluginDirectoryFunc func() string
}

func (m *Manager) ValidateModInstall() error {
	return m.ValidateModInstallFunc()
}

func (m *Manager) InstallMod(url, fullName string) (string, error) {
	return m.InstallModFunc(url, fullName)
}

func (m *Manager) ReplaceMod(url, oldFullName, newFullName string) (file.ModReplacement, error) {
	return m.ReplaceModFunc(url, oldFullName, newFullName)
}

func (m *Manager) RemoveMod(fullName string) error {
	return m.RemoveModFunc(fullName)
}

func (m *Manager) RemoveAllMods() error {
	return m.RemoveAllModsFunc()
}

func (m *Manager) StageModRemoval(fullName string) (file.ModRemoval, error) {
	return m.StageModRemovalFunc(fullName)
}

func (m *Manager) StageAllModsRemoval() (file.ModRemoval, error) {
	return m.StageAllModsRemovalFunc()
}

func (m *Manager) InstallBepInEx(url, fullName string) (string, error) {
	return m.InstallBepInExFunc(url, fullName)
}

func (m *Manager) UpdateBepInEx(url, fullName string) error {
	return m.UpdateBepInExFunc(url, fullName)
}

func (m *Manager) RemoveBepInEx() error {
	return m.RemoveBepInExFunc()
}

func (m *Manager) BepInExPluginDirectory() string {
	return m.BepInExPluginDirectoryFunc()
}
