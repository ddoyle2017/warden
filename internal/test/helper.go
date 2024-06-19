package test

import (
	"testing"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/domain/mod"
)

const (
	DataFolder    = "../../test/data"
	ValheimFolder = "../../test/file"
	ModFullName   = "Azumatt-Where_You_At-1.0.9"

	dbFile = "warden-test.db"
)

func SeedModsTable(t *testing.T, mr repo.Mods, fr repo.Frameworks) []mod.Mod {
	SeedFrameworksTable(t, fr)

	mods := []mod.Mod{
		{
			ID:          1,
			FrameworkID: 1,
			Namespace:   "Azumatt",
			Name:        "Sleepover",
		},
		{
			ID:          2,
			FrameworkID: 1,
			Namespace:   "Azumatt",
			Name:        "Where_You_At",
		},
		{
			ID:          3,
			FrameworkID: 1,
			Namespace:   "Azumatt",
			Name:        "AzuClock",
		},
	}

	for _, m := range mods {
		err := mr.InsertMod(m)
		if err != nil {
			t.Errorf("unexpected error when seeding test database, received: %+v", err)
		}
	}
	return mods
}

func SeedFrameworksTable(t *testing.T, fr repo.Frameworks) []framework.Framework {
	frameworks := []framework.Framework{
		{
			ID:          1,
			Namespace:   "denikson",
			Name:        "BepInExPack_Valheim",
			Version:     "5.4.2202",
			WebsiteURL:  "https://github.com/BepInEx/BepInEx",
			Description: "BepInEx pack for Valheim. Preconfigured and includes unstripped Unity DLLs.",
		},
	}

	for _, f := range frameworks {
		err := fr.InsertFramework(f)
		if err != nil {
			t.Errorf("unexpected error when seeding test database, received: %+v", err)
		}
	}
	return frameworks
}
