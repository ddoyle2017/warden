package file

func NewBackupWithIncompleteCleanup(cleanup func(string) error) Backup {
	b := NewBackup().(*backup)
	b.removeIncomplete = cleanup
	return b
}

func SetModRemovalCleanup(removal ModRemoval, cleanup func(string) error) {
	removal.(*modRemoval).removeStaged = cleanup
}

func RemoveBepInExWithOperations(m Manager, b Backup, removeFiles func(string) error) error {
	manager := m.(*manager)
	manager.backup = b
	return manager.removeBepInEx(removeFiles)
}

func RemoveBackupWithCleanup(b Backup, cleanup func(string) error) error {
	return b.(*backup).remove(cleanup)
}

func InstallModWithBackup(m Manager, b Backup, url, fullName string) (string, error) {
	manager := m.(*manager)
	manager.backup = b
	return manager.InstallMod(url, fullName)
}
