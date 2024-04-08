package mock

import "warden/domain/mod"

// Mods implements the repo.Mods interface and exposes anonymous member functions for mocking
// repo.Mods behavior
type Mods struct {
	ListModsFunc      func() []mod.Mod
	InsertModFunc     func(m mod.Mod) error
	DeleteModFunc     func(modName, namespace string) error
	DeleteAllModsFunc func() error
}

func (mr *Mods) ListMods() []mod.Mod {
	return mr.ListModsFunc()
}

func (mr *Mods) InsertMod(m mod.Mod) error {
	return mr.InsertModFunc(m)
}

func (mr *Mods) DeleteMod(modName, namespace string) error {
	return mr.DeleteModFunc(modName, namespace)
}

func (mr *Mods) DeleteAllMods() error {
	return mr.DeleteAllModsFunc()
}
