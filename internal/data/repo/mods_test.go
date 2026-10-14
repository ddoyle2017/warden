package repo_test

import (
	"database/sql"
	"errors"
	"slices"
	"testing"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestListMods_Happy(t *testing.T) {
	th := helper.NewHelper(t)

	db := th.CreateDatabase()
	repo.CreateModsTable(db)
	repo.CreateFrameworksTable(db)

	mr := repo.NewModsRepo(db)
	fr := repo.NewFrameworksRepo(db)

	expectedMods := th.SeedModsTable(mr, fr)

	results, err := mr.ListMods()
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
		th.DeleteDatabase()
	})
}

func TestListMods_Sad(t *testing.T) {
	tests := map[string]struct {
		setUp func(t *testing.T) repo.Database
	}{
		"given a closed database, when listing mods, then return an error": {
			setUp: func(t *testing.T) repo.Database {
				th := helper.NewHelper(t)
				db := th.CreateDatabase()
				th.DeleteDatabase()
				return db
			},
		},
		"given no mods table, when listing mods, then return an error": {
			setUp: func(t *testing.T) repo.Database {
				return helper.NewHelper(t).CreateDatabase()
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			db := test.setUp(t)

			mr := repo.NewModsRepo(db)

			mods, err := mr.ListMods()
			if !errors.Is(err, repo.ErrModListFailed) {
				t.Errorf("expected error %v, received %v", repo.ErrModListFailed, err)
			}
			if len(mods) != 0 {
				t.Errorf("expected an empty list of mods, received: %+v", mods)
			}
		})
	}

}

func TestGetMod_Happy(t *testing.T) {
	th := helper.NewHelper(t)

	db := th.CreateDatabase()
	repo.CreateModsTable(db)
	repo.CreateFrameworksTable(db)

	mr := repo.NewModsRepo(db)
	fr := repo.NewFrameworksRepo(db)

	mods := th.SeedModsTable(mr, fr)

	m, err := mr.GetMod(mods[0].Name)
	if err != nil {
		t.Error("expected a non-nil error, received nil")
	}
	if !m.Equals(&mods[0]) {
		t.Errorf("expected mod: %+v, received: %+v", mods[0], m)
	}

	t.Cleanup(func() {
		th.DeleteDatabase()
	})
}

func TestGetMod_Sad(t *testing.T) {
	tests := map[string]struct {
		db          repo.Database
		name        string
		expectedErr error
	}{
		"if SQL query fails to execute, return an error": {
			db: &mock.Database{
				QueryFunc: func(_ string, _ ...any) (*sql.Rows, error) {
					return nil, sql.ErrConnDone
				},
			},
			name:        "some_mod",
			expectedErr: repo.ErrModFetchFailed,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			mr := repo.NewModsRepo(test.db)

			m, err := mr.GetMod(test.name)

			if !errors.Is(err, test.expectedErr) {
				t.Errorf("expected error: %+v, received: %+v", test.expectedErr, err)
			}
			if !m.Equals(&mod.Mod{}) {
				t.Errorf("expected an empty mod, received: %+v", m)
			}
		})
	}
}

func TestInsertMod_Happy(t *testing.T) {
	th := helper.NewHelper(t)

	db := th.CreateDatabase()
	repo.CreateModsTable(db)
	repo.CreateFrameworksTable(db)

	mr := repo.NewModsRepo(db)
	fr := repo.NewFrameworksRepo(db)

	expectedMods := th.SeedModsTable(mr, fr)
	newMod := mod.Mod{
		Name:      "X-ray hack",
		Namespace: "Bob",
		Version:   "1.0.0",
	}
	newMod.FilePath = "some/folder/" + newMod.FullName()

	expectedModCount := len(expectedMods) + 1

	err := mr.InsertMod(newMod)
	if err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}
	modList, err := mr.ListMods()
	if err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}
	if len(modList) != expectedModCount {
		t.Errorf("expected %d mods, but found %d mods", expectedModCount, len(modList))
	}
	t.Cleanup(func() {
		th.DeleteDatabase()
	})
}

func TestInsertMod_Sad(t *testing.T) {
	t.Run("given no mods table, when inserting, then return an invalid statement error", func(t *testing.T) {
		db := helper.NewHelper(t).CreateDatabase()
		mr := repo.NewModsRepo(db)

		if err := mr.InsertMod(mod.Mod{}); !errors.Is(err, repo.ErrInvalidStatement) {
			t.Errorf("expected error %v, received %v", repo.ErrInvalidStatement, err)
		}
	})
}

func TestUpdateMod_Happy(t *testing.T) {
	newVersion := "102.23.78"
	tests := map[string]struct {
		id              int
		expectedVersion string
	}{
		"given an unknown mod ID, when updating, then leave installed mods unchanged": {
			id: 999,
		},
		"given an installed mod, when updating, then persist its new version": {
			id:              1,
			expectedVersion: newVersion,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			db := th.CreateDatabase()
			repo.CreateModsTable(db)
			repo.CreateFrameworksTable(db)
			mr := repo.NewModsRepo(db)
			current := th.SeedModsTable(mr, repo.NewFrameworksRepo(db))
			update := current[0]
			update.ID = test.id
			update.Version = newVersion
			expected := slices.Clone(current)
			expected[0].Version = test.expectedVersion

			err := mr.UpdateMod(update)
			if err != nil {
				t.Fatalf("expected a nil error, received: %+v", err)
			}

			mods, err := mr.ListMods()
			if err != nil {
				t.Fatalf("expected a nil error, received: %+v", err)
			}

			areModsEqual := slices.EqualFunc(mods, expected, func(m1, m2 mod.Mod) bool {
				return m1.Equals(&m2)
			})
			if !areModsEqual {
				t.Errorf("expected an updated mods list of: %+v, received: %+v", expected, mods)
			}
		})
	}
}

func TestUpdateMod_Sad(t *testing.T) {
	t.Run("given no mods table, when updating, then return an invalid statement error", func(t *testing.T) {
		db := helper.NewHelper(t).CreateDatabase()
		mr := repo.NewModsRepo(db)

		if err := mr.UpdateMod(mod.Mod{}); !errors.Is(err, repo.ErrInvalidStatement) {
			t.Errorf("expected error %v, received %v", repo.ErrInvalidStatement, err)
		}
	})
}

func TestDeleteMod_Happy(t *testing.T) {
	tests := map[string]struct {
		modName       string
		namespace     string
		expectedCount int
	}{
		"given an unknown mod, when deleting, then leave installed mods unchanged": {
			modName:       "some fake mod name",
			namespace:     "some fake mod author",
			expectedCount: 3,
		},
		"given an installed mod, when deleting, then remove only that mod": {
			modName:       "AzuClock",
			namespace:     "Azumatt",
			expectedCount: 2,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			db := th.CreateDatabase()
			repo.CreateModsTable(db)
			repo.CreateFrameworksTable(db)
			mr := repo.NewModsRepo(db)
			current := th.SeedModsTable(mr, repo.NewFrameworksRepo(db))
			expected := current[:test.expectedCount]

			err := mr.DeleteMod(test.modName, test.namespace)
			if err != nil {
				t.Fatalf("expected a nil error, received: %+v", err)
			}

			mods, err := mr.ListMods()
			if err != nil {
				t.Fatalf("expected a nil error, received: %+v", err)
			}

			areModsEqual := slices.EqualFunc(mods, expected, func(m1, m2 mod.Mod) bool {
				return m1.Equals(&m2)
			})
			if !areModsEqual {
				t.Errorf("expected an updated mods list of: %+v, received: %+v", expected, mods)
			}
		})
	}
}

func TestDeleteMod_Sad(t *testing.T) {
	t.Run("given no mods table, when deleting, then return an invalid statement error", func(t *testing.T) {
		db := helper.NewHelper(t).CreateDatabase()
		mr := repo.NewModsRepo(db)

		if err := mr.DeleteMod("Sleepover", "Azumatt"); !errors.Is(err, repo.ErrInvalidStatement) {
			t.Errorf("expected error %v, received %v", repo.ErrInvalidStatement, err)
		}
	})
}

func TestDeleteAllMods_Happy(t *testing.T) {
	t.Run("given installed mods, when deleting all, then empty the table without dropping it", func(t *testing.T) {
		th := helper.NewHelper(t)
		db := th.CreateDatabase()
		repo.CreateModsTable(db)
		repo.CreateFrameworksTable(db)
		mr := repo.NewModsRepo(db)
		installed := th.SeedModsTable(mr, repo.NewFrameworksRepo(db))

		if err := mr.DeleteAllMods(); err != nil {
			t.Fatal(err)
		}
		mods, err := mr.ListMods()
		if err != nil {
			t.Fatal(err)
		}
		if len(mods) != 0 {
			t.Errorf("expected no installed mods, received %+v", mods)
		}
		if err := mr.InsertMod(installed[0]); err != nil {
			t.Fatalf("expected mods table to remain writable: %v", err)
		}
	})
}
