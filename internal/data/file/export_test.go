package file

func NewBackupWithIncompleteCleanup(cleanup func(string) error) Backup {
	b := NewBackup().(*backup)
	b.removeIncomplete = cleanup
	return b
}

func SetModRemovalCleanup(removal ModRemoval, cleanup func(string) error) {
	removal.(*modRemoval).removeStaged = cleanup
}

func SetModReplacementCleanup(replacement ModReplacement, cleanup func(string) error) {
	replacement.(*modReplacement).removeAll = cleanup
}

func ReplaceModWithOperations(m Manager, url, oldFullName, newFullName string, rename func(string, string) error, removeArchive, removeAll func(string) error) (ModReplacement, error) {
	return m.(*manager).replaceMod(url, oldFullName, newFullName, modReplacementOperations{
		rename: rename, removeArchive: removeArchive, removeAll: removeAll,
	})
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
