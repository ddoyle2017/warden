package service_test

import (
	"errors"
	"io"
	"slices"
	"testing"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/mock"
)

func TestListMods_Happy(t *testing.T) {
	expected := []mod.Mod{
		{
			ID:           1,
			Name:         "Where_You_At",
			Namespace:    "Azumatt",
			FilePath:     "/file/path/test",
			Version:      "1.0.16",
			WebsiteURL:   "www.google.com/some-other-file",
			Description:  "A mod forcing player location on map",
			Dependencies: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
		},
		{
			ID:           2,
			Name:         "Sleepover",
			Namespace:    "Azumatt",
			FilePath:     "/file/path/test",
			Version:      "1.0.1",
			WebsiteURL:   "www.google.com/some-file",
			Description:  "A mod for sleepovers",
			Dependencies: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
		},
	}
	r := mock.ModsRepo{
		ListModsFunc: func() ([]mod.Mod, error) {
			return expected, nil
		},
	}
	ms := service.NewModService(&r, &mock.Manager{}, &mock.Thunderstore{}, &io.LimitedReader{})

	results, err := ms.ListMods()
	if err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}

	areEqual := slices.EqualFunc(expected, results, func(m1, m2 mod.Mod) bool {
		return m1.Equals(&m2)
	})
	if !areEqual {
		t.Errorf("expected mods list: %+v, received: %+v", expected, results)
	}
}

func TestListMods_Sad(t *testing.T) {
	r := mock.ModsRepo{
		ListModsFunc: func() ([]mod.Mod, error) {
			return []mod.Mod{}, repo.ErrModListFailed
		},
	}
	ms := service.NewModService(&r, &mock.Manager{}, &mock.Thunderstore{}, &io.LimitedReader{})

	results, err := ms.ListMods()
	if err == nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}
	if !errors.Is(err, repo.ErrModListFailed) {
		t.Errorf("expected error: %+v, received: %+v", repo.ErrModListFailed, err)
	}

	areEqual := slices.EqualFunc([]mod.Mod{}, results, func(m1, m2 mod.Mod) bool {
		return m1.Equals(&m2)
	})
	if !areEqual {
		t.Errorf("expected empty mods list, received: %+v", results)
	}
}
