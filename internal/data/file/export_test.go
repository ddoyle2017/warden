package file

func NewBackupWithIncompleteCleanup(cleanup func(string) error) Backup {
	b := NewBackup().(*backup)
	b.removeIncomplete = cleanup
	return b
}

func SetModRemovalCleanup(removal ModRemoval, cleanup func(string) error) {
	removal.(*modRemoval).removeStaged = cleanup
}
