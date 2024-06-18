package test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"warden/internal/data/file"
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

func SetUpTestFiles(t *testing.T) {
	source := filepath.Join(DataFolder, ModFullName+".zip")
	destination := filepath.Join(ValheimFolder, file.BepInExPluginDirectory, ModFullName)

	err := file.Unzip(source, destination)
	if err != nil {
		t.Errorf("unexpected error creating test files, received error: %+v", err)
	}
}

func CleanUpTestFiles(t *testing.T) {
	err := os.RemoveAll(ValheimFolder)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unexpected error when cleaning-up test files, received: %+v", err)
	}
	if err := os.MkdirAll(ValheimFolder, os.ModePerm); err != nil {
		t.Errorf("unexpected error creating test file folder, received: %+v", err)
	}
}

func SetUpTestDB(t *testing.T) repo.Database {
	path := filepath.Join(DataFolder, dbFile)

	db, err := repo.OpenDatabase(path)
	if err != nil {
		t.Errorf("unexpected error when creating test database, received: %+v", err)
	}
	return db
}

func RemoveDBFile(t *testing.T) {
	err := os.Remove(filepath.Join(DataFolder, dbFile))
	if errors.Is(err, os.ErrNotExist) {
		// Test database was already removed. This is fine, so we ignore the error and continue.
		return
	}
	if err != nil {
		t.Errorf("unexpected error when cleaning up test database, received error: %+v", err)
	}
}

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
