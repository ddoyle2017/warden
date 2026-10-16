package repo_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/domain/mod"

	"github.com/mattn/go-sqlite3"
)

func TestRepositoryStatementCloseFailure(t *testing.T) {
	for _, failExec := range []bool{false, true} {
		name := "given a pending insert, when statement close fails, then retain the close cause and roll back"
		if failExec {
			name = "given execution and close failures, when returning, then preserve both causes and roll back"
		}
		t.Run(name, func(t *testing.T) {
			closeErr := errors.New("injected statement close failure")
			connector := &closeErrorConnector{
				sqliteDriver: &sqlite3.SQLiteDriver{},
				path:         filepath.Join(t.TempDir(), "warden-test.db"),
				closeErr:     closeErr,
			}
			db := sql.OpenDB(connector)
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			repo.CreateFrameworksTable(db)
			if _, err := db.Exec(`CREATE TABLE close_probe (value INTEGER)`); err != nil {
				t.Fatal(err)
			}
			if failExec {
				// FAIL leaves the trigger's write pending until the transaction rolls back.
				_, err := db.Exec(`CREATE TRIGGER reject_insert BEFORE INSERT ON frameworks
					BEGIN
						INSERT INTO close_probe VALUES (1);
						SELECT RAISE(FAIL, 'execution rejected');
					END`)
				if err != nil {
					t.Fatal(err)
				}
			}

			connector.failClose.Store(true)
			err := repo.NewFrameworksRepo(db).InsertFramework(framework.Framework{
				Name: "Loader", Namespace: "Author", Version: "1.0.0",
			})
			connector.failClose.Store(false)

			if !errors.Is(err, closeErr) {
				t.Errorf("missing statement-close cause: %v", err)
			}
			if errors.Is(err, repo.ErrFrameworkInsertFailed) != failExec {
				t.Errorf("execution sentinel mismatch: %v", err)
			}
			if failExec {
				var cause sqlite3.Error
				if !errors.As(err, &cause) || cause.ExtendedCode != sqlite3.ErrConstraintTrigger {
					t.Errorf("missing original execution cause: %v", err)
				}
			}
			if inUse := db.Stats().InUse; inUse != 0 {
				t.Fatalf("transaction connection leaked: %d", inUse)
			}
			for _, query := range []string{`SELECT COUNT(*) FROM frameworks`, `SELECT COUNT(*) FROM close_probe`} {
				var count int
				if err := db.QueryRow(query).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Errorf("rollback did not undo pending writes: %s returned %d", query, count)
				}
			}
		})
	}
}

func TestFrameworkRemovalStatementCloseFailure(t *testing.T) {
	for name, query := range map[string]string{
		"given a mod-path statement close failure, when removing framework metadata, then retain both tables":     `SELECT id, filePath FROM mods`,
		"given a framework-delete statement close failure, when removing metadata, then roll back both deletions": `DELETE FROM frameworks WHERE name = ?`,
	} {
		t.Run(name, func(t *testing.T) {
			cause := errors.New("statement close interrupted")
			connector := &closeErrorConnector{
				sqliteDriver: &sqlite3.SQLiteDriver{},
				path:         filepath.Join(t.TempDir(), "warden-test.db"), closeErr: cause, closeQuery: query,
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
			f := framework.Framework{Name: framework.BepInEx}
			if err := fr.InsertFramework(f); err != nil {
				t.Fatal(err)
			}
			f, err := fr.GetFramework(f.Name)
			if err != nil {
				t.Fatal(err)
			}
			plugins := t.TempDir()
			m := mod.Mod{Name: "Legacy", FilePath: filepath.Join(plugins, "legacy")}
			if err := mr.InsertMod(m); err != nil {
				t.Fatal(err)
			}
			m, err = mr.GetMod(m.Name)
			if err != nil {
				t.Fatal(err)
			}

			connector.failClose.Store(true)
			err = fr.DeleteFramework(f.Name, plugins)
			connector.failClose.Store(false)
			if !errors.Is(err, cause) {
				t.Errorf("statement-close diagnostic lost: %v", err)
			}
			if actual, err := mr.GetMod(m.Name); err != nil || !actual.Equals(&m) {
				t.Errorf("plugin deletion escaped rollback: %+v, %v", actual, err)
			}
			if actual, err := fr.GetFramework(f.Name); err != nil || actual != f {
				t.Errorf("framework deletion escaped rollback: %+v, %v", actual, err)
			}
			if inUse := db.Stats().InUse; inUse != 0 {
				t.Errorf("transaction connection leaked: %d", inUse)
			}
		})
	}
}

type closeErrorConnector struct {
	sqliteDriver *sqlite3.SQLiteDriver
	path         string
	failClose    atomic.Bool
	closeErr     error
	closeQuery   string
}

func (c *closeErrorConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.sqliteDriver.Open(c.path)
	if err != nil {
		return nil, err
	}
	return &closeErrorConn{Conn: conn, connector: c}, nil
}

func (c *closeErrorConnector) Driver() driver.Driver {
	return c.sqliteDriver
}

type closeErrorConn struct {
	driver.Conn
	connector *closeErrorConnector
}

func (c *closeErrorConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &closeErrorStmt{Stmt: stmt, connector: c.connector, query: query}, nil
}

type closeErrorStmt struct {
	driver.Stmt
	connector *closeErrorConnector
	query     string
}

func (s *closeErrorStmt) Close() error {
	err := s.Stmt.Close()
	if s.connector.failClose.Load() && (s.connector.closeQuery == "" || s.connector.closeQuery == s.query) {
		return errors.Join(err, s.connector.closeErr)
	}
	return err
}
