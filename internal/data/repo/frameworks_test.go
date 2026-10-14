package repo_test

import (
	"database/sql"
	"errors"
	"testing"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestGetFramework_Happy(t *testing.T) {
	th := helper.NewHelper(t)

	db := th.CreateDatabase()
	repo.CreateFrameworksTable(db)

	fr := repo.NewFrameworksRepo(db)
	frameworks := th.SeedFrameworksTable(fr)

	result, err := fr.GetFramework(framework.BepInEx)
	if err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}
	if !result.Equals(&frameworks[0]) {
		t.Errorf("expected framework: %+v, received: %+v", frameworks[0], result)
	}

	t.Cleanup(func() {
		th.DeleteDatabase()
	})
}

func TestGetFramework_Sad(t *testing.T) {
	tests := map[string]struct {
		setUp    func(t *testing.T) repo.Database
		expected error
	}{
		"if query fails to run, return an error": {
			setUp: func(t *testing.T) repo.Database {
				return &mock.Database{
					QueryFunc: func(query string, args ...any) (*sql.Rows, error) {
						return nil, sql.ErrConnDone
					},
				}
			},
			expected: repo.ErrFrameworkFetchFailed,
		},
		"if query returns no results, return an error": {
			setUp: func(t *testing.T) repo.Database {
				db := helper.NewHelper(t).CreateDatabase()
				repo.CreateFrameworksTable(db)
				return db
			},
			expected: repo.ErrFrameworkFetchNoResults,
		},
		"if the query returns multiple results, return an error": {
			setUp: func(t *testing.T) repo.Database {
				th := helper.NewHelper(t)
				db := th.CreateDatabase()
				repo.CreateFrameworksTable(db)

				fr := repo.NewFrameworksRepo(db)
				th.SeedFrameworksTable(fr)
				if err := fr.InsertFramework(framework.Framework{
					Name:      framework.BepInEx,
					Namespace: framework.BepInExNamespace,
				}); err != nil {
					t.Fatal(err)
				}
				return db
			},
			expected: repo.ErrFrameworkFetchMultipleResults,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			db := tt.setUp(t)
			fr := repo.NewFrameworksRepo(db)

			result, err := fr.GetFramework(framework.BepInEx)
			if !result.Equals(&framework.Framework{}) {
				t.Errorf("expected an empty framework, received: %+v", err)
			}
			if !errors.Is(err, tt.expected) {
				t.Errorf("expected error: %+v, received: %+v", tt.expected, err)
			}
		})
	}

}

func TestInsertFramework_Happy(t *testing.T) {
	th := helper.NewHelper(t)

	db := th.CreateDatabase()
	repo.CreateFrameworksTable(db)

	fr := repo.NewFrameworksRepo(transactionOnlyDatabase(t, db))
	f := framework.Framework{
		Name:      framework.BepInEx,
		Namespace: framework.BepInExNamespace,
		Version:   "1.0.0",
	}

	if err := fr.InsertFramework(f); err != nil {
		t.Fatalf("expected a nil error, received: %+v", err)
	}
	f.ID = 1
	stored, err := repo.NewFrameworksRepo(db).GetFramework(f.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Equals(&f) {
		t.Errorf("expected persisted framework %+v, received %+v", f, stored)
	}

	t.Cleanup(func() {
		th.DeleteDatabase()
	})
}

func TestInsertFramework_Sad(t *testing.T) {
	t.Run("given no frameworks table, when inserting, then return an invalid statement error", func(t *testing.T) {
		db := helper.NewHelper(t).CreateDatabase()
		fr := repo.NewFrameworksRepo(db)

		if err := fr.InsertFramework(framework.Framework{}); !errors.Is(err, repo.ErrInvalidStatement) {
			t.Errorf("expected error: %+v, received: %+v", repo.ErrInvalidStatement, err)
		}
	})
}

func TestUpdateFramework_Happy(t *testing.T) {
	tests := map[string]struct {
		installed bool
	}{
		"given no framework, when updating, then leave the table empty": {},
		"given an installed framework, when updating, then persist the new version": {
			installed: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			db := th.CreateDatabase()
			repo.CreateFrameworksTable(db)
			fr := repo.NewFrameworksRepo(db)
			if tt.installed {
				th.SeedFrameworksTable(fr)
			}
			fr = repo.NewFrameworksRepo(transactionOnlyDatabase(t, db))
			update := framework.Framework{
				ID:        1,
				Name:      framework.BepInEx,
				Namespace: framework.BepInExNamespace,
				Version:   "2.0.0",
			}
			if err := fr.UpdateFramework(update); err != nil {
				t.Fatalf("expected a nil error, received: %+v", err)
			}

			result, err := fr.GetFramework(framework.BepInEx)
			if !tt.installed {
				if !errors.Is(err, repo.ErrFrameworkFetchNoResults) {
					t.Errorf("expected no framework, received %+v and error %v", result, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if !result.Equals(&update) {
					t.Errorf("expected updated framework %+v, received %+v", update, result)
				}
			}
		})
	}
}

func TestUpdateFramework_Sad(t *testing.T) {
	t.Run("given no frameworks table, when updating, then return an invalid statement error", func(t *testing.T) {
		db := helper.NewHelper(t).CreateDatabase()
		fr := repo.NewFrameworksRepo(db)

		if err := fr.UpdateFramework(framework.Framework{}); !errors.Is(err, repo.ErrInvalidStatement) {
			t.Errorf("expected error: %+v, received: %+v", repo.ErrInvalidStatement, err)
		}
	})
}

func TestDeleteFramework_Happy(t *testing.T) {
	tests := map[string]struct {
		installed bool
	}{
		"given no framework, when deleting, then leave the table empty": {},
		"given an installed framework, when deleting, then remove its record": {
			installed: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			db := th.CreateDatabase()
			repo.CreateFrameworksTable(db)
			fr := repo.NewFrameworksRepo(db)
			if tt.installed {
				th.SeedFrameworksTable(fr)
			}
			fr = repo.NewFrameworksRepo(transactionOnlyDatabase(t, db))

			if err := fr.DeleteFramework(framework.BepInEx); err != nil {
				t.Fatalf("expected a nil error, received: %+v", err)
			}

			if result, err := fr.GetFramework(framework.BepInEx); !errors.Is(err, repo.ErrFrameworkFetchNoResults) {
				t.Errorf("expected no framework, received %+v and error %v", result, err)
			}
		})
	}
}

func TestDeleteFramework_Sad(t *testing.T) {
	t.Run("given no frameworks table, when deleting, then return an invalid statement error", func(t *testing.T) {
		db := helper.NewHelper(t).CreateDatabase()
		fr := repo.NewFrameworksRepo(db)

		if err := fr.DeleteFramework(framework.BepInEx); !errors.Is(err, repo.ErrInvalidStatement) {
			t.Errorf("expected error: %+v, received: %+v", repo.ErrInvalidStatement, err)
		}
	})
}
