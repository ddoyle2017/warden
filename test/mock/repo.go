package mock

import "warden/domain/mod"

// Repo implements the repo.Repo interface and exposes anonymous member functions for mocking
// repo.Mods behavior
type Repo struct {
	ListModsFunc      func() ([]mod.Mod, error)
	GetModFunc        func(name string) (mod.Mod, error)
	InsertModFunc     func(m mod.Mod) error
	UpdateModFunc     func(m mod.Mod) error
	UpsertModFunc     func(m mod.Mod) error
	DeleteModFunc     func(modName, namespace string) error
	DeleteAllModsFunc func() error
}

func (r *Repo) ListMods() ([]mod.Mod, error) {
	return r.ListModsFunc()
}

func (r *Repo) GetMod(name string) (mod.Mod, error) {
	return r.GetModFunc(name)
}

func (r *Repo) InsertMod(m mod.Mod) error {
	return r.InsertModFunc(m)
}

func (r *Repo) UpdateMod(m mod.Mod) error {
	return r.UpdateModFunc(m)
}

func (r *Repo) UpsertMod(m mod.Mod) error {
	return r.UpsertModFunc(m)
}

func (r *Repo) DeleteMod(modName, namespace string) error {
	return r.DeleteModFunc(modName, namespace)
}

func (r *Repo) DeleteAllMods() error {
	return r.DeleteAllModsFunc()
}
