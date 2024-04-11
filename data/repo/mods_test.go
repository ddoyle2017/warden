package repo_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"warden/data"
	"warden/data/repo"
	"warden/domain/mod"
)

const (
	testDataFolder = "../../test/data"
	testDBFile     = "sqlite-database-test.db"
)

func TestListMods_Happy(t *testing.T) {
	db := setUpTestDB(t)
	data.CreateModsTable(db)

	modsRepo := repo.NewModsRepo(db)
	expectedMods := setUpTestData(t, modsRepo)

	results, err := modsRepo.ListMods()
	if err != nil {
		t.Errorf("unexpected nil error, received: %+v", err)
	}

	compareMod := func(m1, m2 mod.Mod) bool {
		return m1.Equals(&m2)
	}
	if !slices.EqualFunc(results, expectedMods, compareMod) {
		t.Errorf("expected mods: %+v, received mods: %+v", expectedMods, results)
	}

	t.Cleanup(func() {
		removeDBFile(t)
	})
}

func TestListMods_Sad(t *testing.T) {
	tests := map[string]struct {
		setUp func() data.Database
	}{
		"return error if database doesn't exist": {
			setUp: func() data.Database {
				db := setUpTestDB(t)
				removeDBFile(t)
				return db
			},
		},
		"return error if mods table doesn't exist": {
			setUp: func() data.Database {
				return setUpTestDB(t)
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			db := test.setUp()

			mr := repo.NewModsRepo(db)
			mods, err := mr.ListMods()
			if err == nil {
				t.Error("expected a non-nil error, received nil")
			}
			if len(mods) != 0 {
				t.Errorf("expected an empty list of mods, received: %+v", mods)
			}
		})
	}

	t.Cleanup(func() {
		removeDBFile(t)
	})
}

func setUpTestDB(t *testing.T) data.Database {
	path := filepath.Join(testDataFolder, testDBFile)

	db, err := data.OpenDatabase(path)
	if err != nil {
		t.Errorf("unexpected error when creating test database, received: %+v", err)
	}
	return db
}

func setUpTestData(t *testing.T, mr repo.Mods) []mod.Mod {
	mods := []mod.Mod{
		{
			ID:        1,
			Namespace: "Azumatt",
			Name:      "Sleepover",
		},
		{
			ID:        2,
			Namespace: "Azumatt",
			Name:      "Where_You_At"},
		{
			ID:        3,
			Namespace: "Azumatt",
			Name:      "AzuClock",
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

func removeDBFile(t *testing.T) {
	err := os.Remove(filepath.Join(testDataFolder, testDBFile))
	if err != nil {
		t.Errorf("unexpected error when cleaning up test database, received error: %+v", err)
	}
}
