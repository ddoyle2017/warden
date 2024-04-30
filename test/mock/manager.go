package mock

// Manager implements the file.Manager interface and exposes anonymous member functions for mocking
// file.Manager behavior
type Manager struct {
	InstallModFunc       func(url, fullName string) (string, error)
	InstallFrameworkFunc func(url, fullName string) (string, error)
	RemoveModFunc        func(fullName string) error
	RemoveAllModsFunc    func() error
}

func (m *Manager) InstallMod(url, fullName string) (string, error) {
	return m.InstallModFunc(url, fullName)
}

func (m *Manager) InstallFramework(url, fullName string) (string, error) {
	return m.InstallFrameworkFunc(url, fullName)
}

func (m *Manager) RemoveMod(fullName string) error {
	return m.RemoveModFunc(fullName)
}

func (m *Manager) RemoveAllMods() error {
	return m.RemoveAllModsFunc()
}
