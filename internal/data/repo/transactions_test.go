package repo_test

import (
	"database/sql"
	"errors"
	"slices"
	"testing"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/domain/mod"
	"warden/internal/test/helper"
	"warden/internal/test/mock"

	"github.com/mattn/go-sqlite3"
)

func transactionOnlyDatabase(t *testing.T, db repo.Database) *mock.Database {
	t.Helper()
	return &mock.Database{
		QueryFunc: db.Query,
		PrepareFunc: func(string) (*sql.Stmt, error) {
			return nil, errors.New("statement prepared outside its transaction")
		},
		BeginFunc: func() (*sql.Tx, error) {
			tx, err := db.Begin()
			if err != nil {
				return nil, err
			}
			t.Cleanup(func() {
				if err := tx.Rollback(); !errors.Is(err, sql.ErrTxDone) {
					t.Errorf("repository left an unfinished transaction: %v", err)
				}
			})
			return tx, nil
		},
	}
}

func TestRepositoryWriteFailures(t *testing.T) {
	f := framework.Framework{ID: 1, Name: framework.BepInEx, Namespace: framework.BepInExNamespace, Version: "2.0.0"}
	m := mod.Mod{ID: 1, Name: "Sleepover", Namespace: "Azumatt", Version: "2.0.0", FrameworkID: 1}
	writes := map[string]struct {
		table   string
		event   string
		write   func(repo.Database) error
		execErr error
	}{
		"inserting a framework": {
			table: "frameworks", event: "INSERT", execErr: repo.ErrFrameworkInsertFailed,
			write: func(db repo.Database) error { return repo.NewFrameworksRepo(db).InsertFramework(f) },
		},
		"updating a framework": {
			table: "frameworks", event: "UPDATE", execErr: repo.ErrFrameworkUpdateFailed,
			write: func(db repo.Database) error { return repo.NewFrameworksRepo(db).UpdateFramework(f) },
		},
		"deleting a framework": {
			table: "frameworks", event: "DELETE", execErr: repo.ErrFrameworkDeleteFailed,
			write: func(db repo.Database) error { return repo.NewFrameworksRepo(db).DeleteFramework(f.Name) },
		},
		"inserting a mod": {
			table: "mods", event: "INSERT", execErr: repo.ErrModInsertFailed,
			write: func(db repo.Database) error { return repo.NewModsRepo(db).InsertMod(m) },
		},
		"updating a mod": {
			table: "mods", event: "UPDATE", execErr: repo.ErrModUpdateFailed,
			write: func(db repo.Database) error { return repo.NewModsRepo(db).UpdateMod(m) },
		},
		"deleting a mod": {
			table: "mods", event: "DELETE", execErr: repo.ErrModDeleteFailed,
			write: func(db repo.Database) error { return repo.NewModsRepo(db).DeleteMod(m.Name, m.Namespace) },
		},
		"deleting all mods": {
			table: "mods", event: "DELETE", execErr: repo.ErrModDeleteAllFailed,
			write: func(db repo.Database) error { return repo.NewModsRepo(db).DeleteAllMods() },
		},
	}

	for name, tt := range writes {
		t.Run("given a failed begin, when "+name+", then preserve records and both errors", func(t *testing.T) {
			db, mods, f := seededWriteDatabase(t)
			database := transactionOnlyDatabase(t, db)
			cause := errors.New("begin failed")
			database.BeginFunc = func() (*sql.Tx, error) { return nil, cause }

			err := tt.write(database)
			if !errors.Is(err, repo.ErrTransactionFailed) || !errors.Is(err, cause) {
				t.Errorf("expected begin sentinel and cause, received %v", err)
			}
			assertWriteState(t, db, mods, f)
		})

		t.Run("given a missing table, when "+name+", then roll back preparation and retain its cause", func(t *testing.T) {
			db := helper.NewHelper(t).CreateDatabase().(*sql.DB)
			if _, err := db.Exec(`CREATE TABLE write_probe (value INTEGER)`); err != nil {
				t.Fatal(err)
			}
			database := transactionOnlyDatabase(t, db)
			begin := database.BeginFunc
			database.BeginFunc = func() (*sql.Tx, error) {
				tx, err := begin()
				if err != nil {
					return nil, err
				}
				if _, err := tx.Exec(`INSERT INTO write_probe VALUES (1)`); err != nil {
					t.Fatal(err)
				}
				return tx, nil
			}

			err := tt.write(database)
			if !errors.Is(err, repo.ErrInvalidStatement) {
				t.Errorf("expected preparation sentinel, received %v", err)
			}
			var cause sqlite3.Error
			if !errors.As(err, &cause) || cause.Code != sqlite3.ErrError {
				t.Errorf("expected SQLite preparation cause, received %v", err)
			}
			assertWriteProbeEmpty(t, db)
		})

		for _, rollback := range []bool{false, true} {
			action, outcome := "FAIL", "roll back trigger writes and retain the execution cause"
			if rollback {
				action, outcome = "ROLLBACK", "retain both execution and rollback causes"
			}
			t.Run("given a "+action+" trigger, when "+name+", then "+outcome, func(t *testing.T) {
				db, mods, f := seededWriteDatabase(t)
				// FAIL leaves the trigger's earlier write pending until the repository rolls back.
				trigger := `CREATE TRIGGER reject_write BEFORE ` + tt.event + ` ON ` + tt.table + `
					BEGIN
						INSERT INTO write_probe VALUES (-1);
						SELECT RAISE(` + action + `, 'write rejected');
					END`
				if _, err := db.Exec(trigger); err != nil {
					t.Fatal(err)
				}

				err := tt.write(transactionOnlyDatabase(t, db))
				if !errors.Is(err, tt.execErr) {
					t.Errorf("expected execution sentinel %v, received %v", tt.execErr, err)
				}
				wantCodes := []sqlite3.ErrNoExtended{sqlite3.ErrConstraintTrigger}
				if rollback {
					wantCodes = append(wantCodes, sqlite3.ErrError.Extend(0))
				}
				if codes := sqliteErrorCodes(err); !slices.Equal(codes, wantCodes) {
					t.Errorf("expected SQLite causes %v, received %v in %v", wantCodes, codes, err)
				}
				assertWriteState(t, db, mods, f)
			})
		}

		t.Run("given a deferred constraint, when "+name+", then return the commit cause without persisting changes", func(t *testing.T) {
			db, mods, f := seededWriteDatabase(t)
			// Pin the connection-local PRAGMA without allowing outer Prepare to wait for it.
			db.SetMaxOpenConns(1)
			if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
				t.Fatal(err)
			}
			trigger := `CREATE TRIGGER reject_commit AFTER ` + tt.event + ` ON ` + tt.table + `
				BEGIN INSERT INTO write_probe VALUES (-1); END`
			if _, err := db.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			database := transactionOnlyDatabase(t, db)
			begin := database.BeginFunc
			database.BeginFunc = func() (*sql.Tx, error) {
				tx, err := begin()
				if err != nil {
					return nil, err
				}
				if _, err := tx.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
					t.Fatal(err)
				}
				return tx, nil
			}

			err := tt.write(database)
			var cause sqlite3.Error
			if !errors.As(err, &cause) || cause.ExtendedCode != sqlite3.ErrConstraintForeignKey {
				t.Errorf("expected deferred constraint cause, received %v", err)
			}
			if errors.Is(err, tt.execErr) || errors.Is(err, repo.ErrInvalidStatement) || errors.Is(err, repo.ErrTransactionFailed) {
				t.Errorf("commit failure was classified as an earlier failure: %v", err)
			}
			assertWriteState(t, db, mods, f)
		})
	}
}

func seededWriteDatabase(t *testing.T) (*sql.DB, []mod.Mod, framework.Framework) {
	t.Helper()
	th := helper.NewHelper(t)
	db := th.CreateDatabase().(*sql.DB)
	repo.CreateFrameworksTable(db)
	repo.CreateModsTable(db)
	fr := repo.NewFrameworksRepo(db)
	mods := th.SeedModsTable(repo.NewModsRepo(db), fr)
	f, err := fr.GetFramework(framework.BepInEx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE write_probe (
		value INTEGER REFERENCES frameworks(id) DEFERRABLE INITIALLY DEFERRED
	)`); err != nil {
		t.Fatal(err)
	}
	return db, mods, f
}

func assertWriteState(t *testing.T, db *sql.DB, wantMods []mod.Mod, wantFramework framework.Framework) {
	t.Helper()
	assertWriteProbeEmpty(t, db)
	mods, err := repo.NewModsRepo(db).ListMods()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(mods, wantMods, func(a, b mod.Mod) bool { return a.Equals(&b) }) {
		t.Errorf("expected unchanged mods %v, received %v", wantMods, mods)
	}
	f, err := repo.NewFrameworksRepo(db).GetFramework(wantFramework.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Equals(&wantFramework) {
		t.Errorf("expected unchanged framework %v, received %v", wantFramework, f)
	}
}

func assertWriteProbeEmpty(t *testing.T, db *sql.DB) {
	t.Helper()
	if inUse := db.Stats().InUse; inUse != 0 {
		t.Fatalf("expected all transaction connections released, found %d in use", inUse)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM write_probe`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected no persisted probe writes, received %d", count)
	}
}

func sqliteErrorCodes(err error) []sqlite3.ErrNoExtended {
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		var codes []sqlite3.ErrNoExtended
		for _, cause := range e.Unwrap() {
			codes = append(codes, sqliteErrorCodes(cause)...)
		}
		return codes
	case interface{ Unwrap() error }:
		return sqliteErrorCodes(e.Unwrap())
	default:
		var cause sqlite3.Error
		if errors.As(err, &cause) {
			return []sqlite3.ErrNoExtended{cause.ExtendedCode}
		}
		return nil
	}
}
