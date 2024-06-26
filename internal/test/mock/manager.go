package mock

// Manager implements the file.Manager interface and exposes anonymous member functions for mocking
// file.Manager behavior
type Manager struct {
	InstallModFunc     func(url, fullName string) (string, error)
	RemoveModFunc      func(fullName string) error
	RemoveAllModsFunc  func() error
	InstallBepInExFunc func(url, fullName string) (string, error)
	UpdateBepInExFunc  func(url, fullName string) error
	RemoveBepInExFunc  func() error
}

func (m *Manager) InstallMod(url, fullName string) (string, error) {
	return m.InstallModFunc(url, fullName)
}

func (m *Manager) RemoveMod(fullName string) error {
	return m.RemoveModFunc(fullName)
}

func (m *Manager) RemoveAllMods() error {
	return m.RemoveAllModsFunc()
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
