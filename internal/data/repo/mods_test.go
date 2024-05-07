package repo_test

import (
	"database/sql"
	"errors"
	"slices"
	"testing"
	"warden/internal/data"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
	"warden/internal/test"
	"warden/internal/test/mock"
)

func TestListMods_Happy(t *testing.T) {
	db := test.SetUpTestDB(t)
	data.CreateModsTable(db)
	data.CreateFrameworksTable(db)

	mr := repo.NewModsRepo(db)
	fr := repo.NewFrameworksRepo(db)

	expectedMods := test.SeedModsTable(t, mr, fr)

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
		test.RemoveDBFile(t)
	})
}

func TestListMods_Sad(t *testing.T) {
	tests := map[string]struct {
		setUp func() data.Database
	}{
		"return error if database doesn't exist": {
			setUp: func() data.Database {
				db := test.SetUpTestDB(t)
				test.RemoveDBFile(t)
				return db
			},
		},
		"return error if mods table doesn't exist": {
			setUp: func() data.Database {
				return test.SetUpTestDB(t)
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
		test.RemoveDBFile(t)
	})
}

func TestGetMod_Happy(t *testing.T) {
	db := test.SetUpTestDB(t)
	data.CreateModsTable(db)
	data.CreateFrameworksTable(db)

	mr := repo.NewModsRepo(db)
	fr := repo.NewFrameworksRepo(db)

	mods := test.SeedModsTable(t, mr, fr)

	m, err := mr.GetMod(mods[0].Name)
	if err != nil {
		t.Error("expected a non-nil error, received nil")
	}
	if !m.Equals(&mods[0]) {
		t.Errorf("expected mod: %+v, received: %+v", mods[0], m)
	}

	t.Cleanup(func() {
		test.RemoveDBFile(t)
	})
}

func TestGetMod_Sad(t *testing.T) {
	tests := map[string]struct {
		db          data.Database
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

	t.Cleanup(func() {
		test.RemoveDBFile(t)
	})
}

func TestInsertMod_Happy(t *testing.T) {
	db := test.SetUpTestDB(t)
	data.CreateModsTable(db)
	data.CreateFrameworksTable(db)

	mr := repo.NewModsRepo(db)
	fr := repo.NewFrameworksRepo(db)

	expectedMods := test.SeedModsTable(t, mr, fr)
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
		test.RemoveDBFile(t)
	})
}

func TestInsertMod_Sad(t *testing.T) {
	tests := map[string]struct {
		db          data.Database
		expectedErr error
	}{
		"returns error when unable to prepare an INSERT SQL statement": {
			db: &mock.Database{
				PrepareFunc: func(_ string) (*sql.Stmt, error) {
					return nil, errors.New("invalid SQL")
				},
			},
			expectedErr: repo.ErrInvalidStatement,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			mr := repo.NewModsRepo(test.db)

			err := mr.InsertMod(mod.Mod{})
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("expected error: %+v, received: %+v", test.expectedErr, err)
			}
		})
	}

	t.Cleanup(func() {
		test.RemoveDBFile(t)
	})
}

func TestUpdateMod_Happy(t *testing.T) {
	db := test.SetUpTestDB(t)
	data.CreateModsTable(db)
	data.CreateFrameworksTable(db)

	mr := repo.NewModsRepo(db)
	fr := repo.NewFrameworksRepo(db)

	currentMods := test.SeedModsTable(t, mr, fr)
	newVersion := "102.23.78"

	tests := map[string]struct {
		m        mod.Mod
		expected []mod.Mod
	}{
		"if mod isn't found, update nothing and return successful": {
			m: mod.Mod{
				ID:      12738923789127,
				Version: "0.0.2",
			},
			expected: currentMods,
		},
		"if mod is found, apply updates and return successful": {
			m: mod.Mod{
				ID:        1,
				Namespace: "Azumatt",
				Name:      "Sleepover",
				Version:   newVersion,
			},
			expected: []mod.Mod{
				{
					ID:          1,
					FrameworkID: 1,
					Namespace:   "Azumatt",
					Name:        "Sleepover",
					Version:     newVersion,
				},
				{
					ID:          2,
					FrameworkID: 1,
					Namespace:   "Azumatt",
					Name:        "Where_You_At"},
				{
					ID:          3,
					FrameworkID: 1,
					Namespace:   "Azumatt",
					Name:        "AzuClock",
				},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := mr.UpdateMod(test.m)
			if err != nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}

			mods, err := mr.ListMods()
			if err != nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}

			areModsEqual := slices.EqualFunc(mods, test.expected, func(m1, m2 mod.Mod) bool {
				return m1.Equals(&m2)
			})
			if !areModsEqual {
				t.Errorf("expected an updated mods list of: %+v, received: %+v", test.expected, mods)
			}
		})
	}

	t.Cleanup(func() {
		test.RemoveDBFile(t)
	})
}

func TestUpdateMod_Sad(t *testing.T) {
	tests := map[string]struct {
		db          data.Database
		expectedErr error
	}{
		"returns error when unable to prepare an UPDATE SQL statement": {
			db: &mock.Database{
				PrepareFunc: func(_ string) (*sql.Stmt, error) {
					return nil, errors.New("invalid SQL")
				},
			},
			expectedErr: repo.ErrInvalidStatement,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			mr := repo.NewModsRepo(test.db)

			err := mr.UpdateMod(mod.Mod{})
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("expected error: %+v, received: %+v", test.expectedErr, err)
			}
		})
	}

	t.Cleanup(func() {
		test.RemoveDBFile(t)
	})
}

func TestDeleteMod_Happy(t *testing.T) {
	db := test.SetUpTestDB(t)
	data.CreateModsTable(db)
	data.CreateFrameworksTable(db)

	mr := repo.NewModsRepo(db)
	fr := repo.NewFrameworksRepo(db)

	currentMods := test.SeedModsTable(t, mr, fr)
	last := len(currentMods) - 1

	tests := map[string]struct {
		modName   string
		namespace string
		expected  []mod.Mod
	}{
		"if mod isn't found, delete nothing and return successful": {
			modName:   "some fake mod name",
			namespace: "some fake mod author",
			expected:  currentMods,
		},
		"if mod is found, delete it and return successful": {
			modName:   currentMods[last].Name,
			namespace: currentMods[last].Namespace,
			expected: []mod.Mod{
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
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := mr.DeleteMod(test.modName, test.namespace)
			if err != nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}

			mods, err := mr.ListMods()
			if err != nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}

			areModsEqual := slices.EqualFunc(mods, test.expected, func(m1, m2 mod.Mod) bool {
				return m1.Equals(&m2)
			})
			if !areModsEqual {
				t.Errorf("expected an updated mods list of: %+v, received: %+v", test.expected, mods)
			}
		})
	}

	t.Cleanup(func() {
		test.RemoveDBFile(t)
	})
}

func TestDeleteMod_Sad(t *testing.T) {
	tests := map[string]struct {
		db          data.Database
		expectedErr error
	}{
		"returns error when unable to prepare a DELETE SQL statement": {
			db: &mock.Database{
				PrepareFunc: func(_ string) (*sql.Stmt, error) {
					return nil, errors.New("invalid SQL")
				},
			},
			expectedErr: repo.ErrInvalidStatement,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			mr := repo.NewModsRepo(test.db)

			err := mr.DeleteMod("Sleepover", "Azumatt")
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("expected error: %+v, received: %+v", test.expectedErr, err)
			}
		})
	}

	t.Cleanup(func() {
		test.RemoveDBFile(t)
	})
}

func TestDeleteAllMods_Happy(t *testing.T) {
	t.Cleanup(func() {
		test.RemoveDBFile(t)
	})
}
