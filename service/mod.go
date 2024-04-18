package service

import (
	"errors"
	"warden/api/thunderstore"
	"warden/data/file"
	"warden/data/repo"
	"warden/domain/mod"
)

var (
	ErrUnableToListMods = errors.New("unable to list mods")
)

// ModService encapsulates all the business logic for managing mods.
type ModService interface {
	ListMods() ([]mod.Mod, error)
	AddMod()
	UpdateMod()
}

type modService struct {
	r  repo.Mods
	fm file.Manager
	ts thunderstore.Thunderstore
}

func NewModService(r repo.Mods, fm file.Manager, ts thunderstore.Thunderstore) ModService {
	return &modService{
		r:  r,
		fm: fm,
		ts: ts,
	}
}

func (ms *modService) ListMods() ([]mod.Mod, error) {
	return ms.r.ListMods()
}

func (ms *modService) AddMod() {

}

func (ms *modService) UpdateMod() {

}
