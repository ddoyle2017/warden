package mock

import "warden/domain/mod"

// ModsRepo implements the repo.ModsRepo interface and exposes anonymous member functions for mocking
// repo.Mods behavior
type ModsRepo struct {
	ListModsFunc      func() ([]mod.Mod, error)
	GetModFunc        func(name string) (mod.Mod, error)
	InsertModFunc     func(m mod.Mod) error
	UpdateModFunc     func(m mod.Mod) error
	UpsertModFunc     func(m mod.Mod) error
	DeleteModFunc     func(modName, namespace string) error
	DeleteAllModsFunc func() error
}

func (r *ModsRepo) ListMods() ([]mod.Mod, error) {
	return r.ListModsFunc()
}

func (r *ModsRepo) GetMod(name string) (mod.Mod, error) {
	return r.GetModFunc(name)
}

func (r *ModsRepo) InsertMod(m mod.Mod) error {
	return r.InsertModFunc(m)
}

func (r *ModsRepo) UpdateMod(m mod.Mod) error {
	return r.UpdateModFunc(m)
}

func (r *ModsRepo) UpsertMod(m mod.Mod) error {
	return r.UpsertModFunc(m)
}

func (r *ModsRepo) DeleteMod(modName, namespace string) error {
	return r.DeleteModFunc(modName, namespace)
}

func (r *ModsRepo) DeleteAllMods() error {
	return r.DeleteAllModsFunc()
}
