package repo_test

import (
	"database/sql"
	"errors"
	"testing"
	"warden/internal/data"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/test"
	"warden/internal/test/mock"
)

func TestGetFramework_Happy(t *testing.T) {
	db := test.SetUpTestDB(t)
	data.CreateFrameworksTable(db)

	fr := repo.NewFrameworksRepo(db)
	frameworks := test.SeedFrameworksTable(t, fr)

	result, err := fr.GetFramework(framework.BepInEx)
	if err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}
	if !result.Equals(&frameworks[0]) {
		t.Errorf("expected framework: %+v, received: %+v", frameworks[0], result)
	}

	t.Cleanup(func() {
		test.RemoveDBFile(t)
	})
}

func TestGetFramework_Sad(t *testing.T) {
	tests := map[string]struct {
		setUp    func() data.Database
		expected error
	}{
		"if query fails to run, return an error": {
			setUp: func() data.Database {
				return &mock.Database{
					QueryFunc: func(query string, args ...any) (*sql.Rows, error) {
						return nil, sql.ErrConnDone
					},
				}
			},
			expected: repo.ErrFrameworkFetchFailed,
		},
		"if query returns no results, return an error": {
			setUp: func() data.Database {
				db := test.SetUpTestDB(t)
				data.CreateFrameworksTable(db)
				return db
			},
			expected: repo.ErrFrameworkFetchNoResults,
		},
		"if the query returns multiple results, return an error": {
			setUp: func() data.Database {
				db := test.SetUpTestDB(t)
				data.CreateFrameworksTable(db)

				fr := repo.NewFrameworksRepo(db)
				test.SeedFrameworksTable(t, fr)
				fr.InsertFramework(framework.Framework{
					Name:      framework.BepInEx,
					Namespace: framework.BepInExNamespace,
				})
				return db
			},
			expected: repo.ErrFrameworkFetchMultipleResults,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			db := tt.setUp()
			fr := repo.NewFrameworksRepo(db)

			result, err := fr.GetFramework(framework.BepInEx)
			if !result.Equals(&framework.Framework{}) {
				t.Errorf("expected an empty framework, received: %+v", err)
			}
			if !errors.Is(err, tt.expected) {
				t.Errorf("expected error: %+v, received: %+v", tt.expected, err)
			}
			t.Cleanup(func() {
				test.RemoveDBFile(t)
			})
		})
	}

}

func TestInsertFramework_Happy(t *testing.T) {
	db := test.SetUpTestDB(t)
	data.CreateFrameworksTable(db)

	fr := repo.NewFrameworksRepo(db)
	f := framework.Framework{
		Name:      framework.BepInEx,
		Namespace: framework.BepInExNamespace,
		Version:   "1.0.0",
	}

	if err := fr.InsertFramework(f); err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}

	t.Cleanup(func() {
		test.RemoveDBFile(t)
	})
}

func TestInsertFramework_Sad(t *testing.T) {
	db := &mock.Database{
		PrepareFunc: func(query string) (*sql.Stmt, error) {
			return nil, sql.ErrConnDone
		},
	}
	fr := repo.NewFrameworksRepo(db)

	if err := fr.InsertFramework(framework.Framework{}); !errors.Is(err, repo.ErrInvalidStatement) {
		t.Errorf("expected error: %+v, received: %+v", repo.ErrInvalidStatement, err)
	}

	t.Cleanup(func() {
		test.RemoveDBFile(t)
	})
}

func TestUpdateFramework_Happy(t *testing.T) {
	tests := map[string]struct {
		setUp func() data.Database
	}{
		"if framework isn't found, update nothing and return successful": {
			setUp: func() data.Database {
				db := test.SetUpTestDB(t)
				data.CreateFrameworksTable(db)
				return db
			},
		},
		"if framework is found, update framework and return successful": {
			setUp: func() data.Database {
				db := test.SetUpTestDB(t)
				data.CreateFrameworksTable(db)

				test.SeedFrameworksTable(t, repo.NewFrameworksRepo(db))
				return db
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			db := tt.setUp()
			update := framework.Framework{
				Name:      framework.BepInEx,
				Namespace: framework.BepInExNamespace,
				Version:   "2.0.0",
			}
			fr := repo.NewFrameworksRepo(db)

			if err := fr.UpdateFramework(update); err != nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}

			t.Cleanup(func() {
				test.RemoveDBFile(t)
			})
		})
	}
}

func TestUpdateFramework_Sad(t *testing.T) {
	db := &mock.Database{
		PrepareFunc: func(query string) (*sql.Stmt, error) {
			return nil, sql.ErrConnDone
		},
	}
	fr := repo.NewFrameworksRepo(db)

	if err := fr.UpdateFramework(framework.Framework{}); !errors.Is(err, repo.ErrInvalidStatement) {
		t.Errorf("expected error: %+v, received: %+v", repo.ErrInvalidStatement, err)
	}

	t.Cleanup(func() {
		test.RemoveDBFile(t)
	})
}

func TestDeleteFramework_Happy(t *testing.T) {
	tests := map[string]struct {
		setUp func() data.Database
	}{
		"if no record is found, skip delete and return successful": {
			setUp: func() data.Database {
				db := test.SetUpTestDB(t)
				data.CreateFrameworksTable(db)
				return db
			},
		},
		"if record is found, delete it and return successful": {
			setUp: func() data.Database {
				db := test.SetUpTestDB(t)
				data.CreateFrameworksTable(db)
				test.SeedFrameworksTable(t, repo.NewFrameworksRepo(db))
				return db
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			db := tt.setUp()
			fr := repo.NewFrameworksRepo(db)

			if err := fr.DeleteFramework(framework.BepInEx); err != nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}

			t.Cleanup(func() {
				test.RemoveDBFile(t)
			})
		})
	}
}

func TestDeleteFramework_Sad(t *testing.T) {
	db := &mock.Database{
		PrepareFunc: func(query string) (*sql.Stmt, error) {
			return nil, sql.ErrConnDone
		},
	}
	fr := repo.NewFrameworksRepo(db)

	if err := fr.DeleteFramework(framework.BepInEx); !errors.Is(err, repo.ErrInvalidStatement) {
		t.Errorf("expected error: %+v, received: %+v", repo.ErrInvalidStatement, err)
	}

	t.Cleanup(func() {
		test.RemoveDBFile(t)
	})
}
