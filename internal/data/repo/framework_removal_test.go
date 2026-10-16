package repo_test

import (
	"cmp"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/domain/mod"
	"warden/internal/test/helper"

	"github.com/mattn/go-sqlite3"
)

func TestFrameworkRemovalPathScope(t *testing.T) {
	for _, relativeDirectory := range []bool{false, true} {
		name := "given absolute installation paths, when deleting a framework, then remove only records within its plugin tree"
		if relativeDirectory {
			name = "given a relative plugin directory, when deleting a framework, then match normalized installation paths"
		}
		t.Run(name, func(t *testing.T) {
			db := helper.NewHelper(t).CreateDatabase()
			repo.CreateFrameworksTable(db)
			repo.CreateModsTable(db)
			fr, mr := repo.NewFrameworksRepo(db), repo.NewModsRepo(db)
			for _, name := range []string{framework.BepInEx, "OtherFramework"} {
				if err := fr.InsertFramework(framework.Framework{Name: name}); err != nil {
					t.Fatal(err)
				}
			}
			directory := filepath.Join(t.TempDir(), "plugins_%'")
			prefix := directory + string(filepath.Separator)
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			relativePath, err := filepath.Rel(cwd, filepath.Join(directory, "relative-plugin"))
			if err != nil {
				t.Fatal(err)
			}
			cases := []struct {
				name    string
				path    string
				removed bool
			}{
				{name: "Root", path: directory, removed: true},
				{name: "Nested", path: filepath.Join(directory, "nested", "plugin"), removed: true},
				{name: "Normalized", path: prefix + "nested/../plugin", removed: true},
				{name: "Relative", path: relativePath, removed: true},
				{name: "Sibling", path: directory + "-else/plugin"},
				{name: "Parent", path: filepath.Dir(directory)},
				{name: "Escaping", path: prefix + "../outside/plugin"},
				{name: "Empty"},
			}
			var retained []mod.Mod
			for _, tt := range cases {
				m := mod.Mod{Name: tt.name, Namespace: "Owner", FilePath: tt.path}
				if err := mr.InsertMod(m); err != nil {
					t.Fatal(err)
				}
				m, err := mr.GetMod(m.Name)
				if err != nil {
					t.Fatal(err)
				}
				if !tt.removed {
					retained = append(retained, m)
				}
			}
			if relativeDirectory {
				directory, err = filepath.Rel(cwd, directory)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.NewFrameworksRepo(transactionOnlyDatabase(t, db)).DeleteFramework(framework.BepInEx, directory); err != nil {
				t.Fatal(err)
			}
			actual, err := mr.ListMods()
			compare := func(a, b mod.Mod) int { return cmp.Compare(a.ID, b.ID) }
			slices.SortFunc(actual, compare)
			slices.SortFunc(retained, compare)
			if err != nil || !slices.EqualFunc(actual, retained, func(a, b mod.Mod) bool { return a.Equals(&b) }) {
				t.Errorf("remaining mods = %+v, %v; want %+v", actual, err, retained)
			}
			if _, err := fr.GetFramework(framework.BepInEx); !errors.Is(err, repo.ErrFrameworkFetchNoResults) {
				t.Errorf("removed framework still recorded: %v", err)
			}
			if other, err := fr.GetFramework("OtherFramework"); err != nil || other.Name != "OtherFramework" {
				t.Errorf("unrelated framework changed: %+v, %v", other, err)
			}
		})
	}
}

func TestFrameworkRemovalRequiresScope(t *testing.T) {
	t.Run("given an empty plugin directory, when deleting framework metadata, then refuse unscoped deletion", func(t *testing.T) {
		db, mods, f := seededWriteDatabase(t)
		err := repo.NewFrameworksRepo(db).DeleteFramework(f.Name, "")
		if !errors.Is(err, repo.ErrFrameworkDeleteFailed) {
			t.Errorf("unscoped deletion was not refused: %v", err)
		}
		assertWriteState(t, db, mods, f)
	})
}

func TestFrameworkRemovalTransactionFailures(t *testing.T) {
	for name, tt := range map[string]struct {
		table     string
		action    string
		sentinel  error
		beginFail bool
		deferred  bool
	}{
		"given a failed begin, when removing framework metadata, then preserve both tables and the cause": {
			beginFail: true, sentinel: repo.ErrTransactionFailed,
		},
		"given a rejected plugin deletion, when removing framework metadata, then undo pending plugin writes": {
			table: "mods", action: "FAIL", sentinel: repo.ErrModDeleteFailed,
		},
		"given a rejected framework deletion, when removing metadata, then roll back the already deleted plugins": {
			table: "frameworks", action: "FAIL", sentinel: repo.ErrFrameworkDeleteFailed,
		},
		"given a trigger rollback, when removing metadata, then retain the execution and rollback diagnostics": {
			table: "frameworks", action: "ROLLBACK", sentinel: repo.ErrFrameworkDeleteFailed,
		},
		"given a deferred constraint, when committing framework removal, then retain both tables and the commit cause": {
			table: "frameworks", deferred: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			db, mods, f := seededWriteDatabase(t)
			plugins := filepath.Join(t.TempDir(), "plugins")
			for i := range mods {
				mods[i].FilePath = filepath.Join(plugins, mods[i].FullName())
				if err := repo.NewModsRepo(db).UpdateMod(mods[i]); err != nil {
					t.Fatal(err)
				}
			}
			database := transactionOnlyDatabase(t, db)
			database.QueryFunc = func(string, ...any) (*sql.Rows, error) {
				return nil, errors.New("query executed outside its transaction")
			}
			beginCause := errors.New("begin interrupted")
			if tt.beginFail {
				database.BeginFunc = func() (*sql.Tx, error) { return nil, beginCause }
			} else {
				reject := "SELECT RAISE(" + tt.action + ", 'deletion rejected');"
				if tt.deferred {
					db.SetMaxOpenConns(1)
					if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
						t.Fatal(err)
					}
					reject = ""
				}
				// AFTER + FAIL leaves earlier row/trigger writes pending until rollback.
				trigger := `CREATE TRIGGER reject_removal AFTER DELETE ON ` + tt.table + `
					BEGIN INSERT INTO write_probe VALUES (-1); ` + reject + ` END`
				if _, err := db.Exec(trigger); err != nil {
					t.Fatal(err)
				}
			}

			err := repo.NewFrameworksRepo(database).DeleteFramework(f.Name, plugins)
			if tt.beginFail {
				if !errors.Is(err, tt.sentinel) || !errors.Is(err, beginCause) {
					t.Errorf("begin diagnostic lost: %v", err)
				}
			} else if tt.deferred {
				var cause sqlite3.Error
				if !errors.As(err, &cause) || cause.ExtendedCode != sqlite3.ErrConstraintForeignKey ||
					!strings.Contains(err.Error(), "commit transaction") {
					t.Errorf("commit diagnostic lost: %v", err)
				}
			} else {
				wantCodes := []sqlite3.ErrNoExtended{sqlite3.ErrConstraintTrigger}
				if tt.action == "ROLLBACK" {
					wantCodes = append(wantCodes, sqlite3.ErrError.Extend(0))
				}
				codes := sqliteErrorCodes(err)
				slices.Sort(codes)
				slices.Sort(wantCodes)
				if !errors.Is(err, tt.sentinel) || !slices.Equal(codes, wantCodes) {
					t.Errorf("execution/rollback diagnostics lost: %v", err)
				}
			}
			assertWriteState(t, db, mods, f)
		})
	}
}

func TestFrameworkRemovalPreparationRollback(t *testing.T) {
	t.Run("given a missing framework table, when its deletion cannot be prepared, then restore the deleted plugin rows", func(t *testing.T) {
		db := helper.NewHelper(t).CreateDatabase()
		repo.CreateModsTable(db)
		plugins := t.TempDir()
		mr := repo.NewModsRepo(db)
		m := mod.Mod{Name: "Legacy", FilePath: filepath.Join(plugins, "legacy")}
		if err := mr.InsertMod(m); err != nil {
			t.Fatal(err)
		}
		m, err := mr.GetMod(m.Name)
		if err != nil {
			t.Fatal(err)
		}

		err = repo.NewFrameworksRepo(transactionOnlyDatabase(t, db)).DeleteFramework(framework.BepInEx, plugins)
		var cause sqlite3.Error
		if !errors.Is(err, repo.ErrInvalidStatement) || !errors.As(err, &cause) {
			t.Errorf("preparation diagnostic lost: %v", err)
		}
		if actual, err := mr.GetMod(m.Name); err != nil || !actual.Equals(&m) {
			t.Errorf("plugin deletion escaped rollback: %+v, %v; want %+v", actual, err, m)
		}
	})
}

func TestFrameworkRemovalCommitUncertainty(t *testing.T) {
	t.Run("given a commit that succeeds but reports an error, when deleting metadata, then preserve the error without claiming rollback", func(t *testing.T) {
		cause := errors.New("commit acknowledgement lost")
		connector := &commitErrorConnector{
			path: filepath.Join(t.TempDir(), "warden-test.db"), cause: cause,
		}
		db := sql.OpenDB(connector)
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		})
		repo.CreateFrameworksTable(db)
		repo.CreateModsTable(db)
		fr, mr := repo.NewFrameworksRepo(db), repo.NewModsRepo(db)
		plugins := t.TempDir()
		if err := fr.InsertFramework(framework.Framework{Name: framework.BepInEx}); err != nil {
			t.Fatal(err)
		}
		if err := mr.InsertMod(mod.Mod{Name: "Legacy", FilePath: filepath.Join(plugins, "legacy")}); err != nil {
			t.Fatal(err)
		}

		connector.failCommit.Store(true)
		err := fr.DeleteFramework(framework.BepInEx, plugins)
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), "commit transaction") {
			t.Errorf("uncertain commit cause lost: %v", err)
		}
		if mods, err := mr.ListMods(); err != nil || len(mods) != 0 {
			t.Errorf("committed plugin deletion was not visible: %+v, %v", mods, err)
		}
		if _, err := fr.GetFramework(framework.BepInEx); !errors.Is(err, repo.ErrFrameworkFetchNoResults) {
			t.Errorf("committed framework deletion was not visible: %v", err)
		}
		if inUse := db.Stats().InUse; inUse != 0 {
			t.Errorf("transaction connection leaked: %d", inUse)
		}
	})
}

type commitErrorConnector struct {
	path       string
	cause      error
	failCommit atomic.Bool
}

func (c *commitErrorConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.Driver().Open(c.path)
	if err != nil {
		return nil, err
	}
	return &commitErrorConn{Conn: conn, connector: c}, nil
}

func (c *commitErrorConnector) Driver() driver.Driver { return &sqlite3.SQLiteDriver{} }

type commitErrorConn struct {
	driver.Conn
	connector *commitErrorConnector
}

func (c *commitErrorConn) Begin() (driver.Tx, error) {
	tx, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}
	return &commitErrorTx{Tx: tx, connector: c.connector}, nil
}

type commitErrorTx struct {
	driver.Tx
	connector *commitErrorConnector
}

func (t *commitErrorTx) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	if t.connector.failCommit.Load() {
		return t.connector.cause
	}
	return nil
}
