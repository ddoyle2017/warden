package repo

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
)

func TestReadOnlyPhysicalConnections(t *testing.T) {
	for _, wal := range []bool{false, true} {
		name := "given a discarded connection, when reconnecting, then restore query-only defenses and close SQL resources"
		if wal {
			name = "given WAL after a connection is discarded, when reconnecting, then reject before driver Open"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "warden.db")
			initializeConnectionFixture(t, path)
			reader, err := OpenReadOnlyDatabase(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := reader.Close(); err != nil {
					t.Error(err)
				}
			})
			if _, err := reader.db.Exec(`PRAGMA query_only = OFF`); err != nil {
				t.Fatal(err)
			}
			conn, err := reader.db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := conn.Raw(func(any) error { return driver.ErrBadConn }); !errors.Is(err, driver.ErrBadConn) {
				t.Fatal(err)
			}
			if err := conn.Close(); !errors.Is(err, sql.ErrConnDone) {
				t.Fatal(err)
			}
			if got := reader.db.Stats().OpenConnections; got != 0 {
				t.Fatalf("discarded connection still open: %d", got)
			}
			if wal {
				writer, err := sql.Open("sqlite3", path)
				if err != nil {
					t.Fatal(err)
				}
				_, execErr := writer.Exec(`PRAGMA journal_mode = WAL`)
				if err := errors.Join(execErr, writer.Close()); err != nil {
					t.Fatal(err)
				}
			}
			openCalls := 0
			reader.connector.sqlite.ConnectHook = func(*sqlite3.SQLiteConn) error {
				openCalls++
				return nil
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			directoryBefore, err := os.Stat(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			mods, err := NewModsRepo(reader).ListModsContext(context.Background())
			if wal {
				if mods != nil || !errors.Is(err, ErrDatabaseUnsupported) || openCalls != 0 {
					t.Fatalf("WAL reconnect reached driver or succeeded: %+v, %v, opens=%d", mods, err, openCalls)
				}
			} else {
				if err != nil || len(mods) != 1 || openCalls != 1 {
					t.Fatalf("reconnect = %+v, %v, opens=%d", mods, err, openCalls)
				}
				var queryOnly, busyTimeout int
				if err := reader.db.QueryRow(`PRAGMA query_only`).Scan(&queryOnly); err != nil {
					t.Fatal(err)
				}
				if err := reader.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
					t.Fatal(err)
				}
				if queryOnly != 1 || busyTimeout != 0 || reader.db.Stats().InUse != 0 {
					t.Errorf("defenses or rows leaked: query_only=%d busy_timeout=%d stats=%+v", queryOnly, busyTimeout, reader.db.Stats())
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("reconnect changed database bytes: %v", err)
			}
			directoryAfter, err := os.Stat(filepath.Dir(path))
			if err != nil || !directoryAfter.ModTime().Equal(directoryBefore.ModTime()) {
				t.Fatalf("reconnect created transient sidecars: %v", err)
			}
			if err := reader.Close(); err != nil || reader.db.Stats().OpenConnections != 0 {
				t.Fatalf("close leaked connections: %v, %+v", err, reader.db.Stats())
			}
		})
	}
}

func TestReadOnlyCloseRetainsReservationUntilRowsClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warden.db")
	initializeConnectionFixture(t, path)
	reader, err := OpenReadOnlyDatabase(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := reader.Query(`SELECT * FROM mods`)
	if err != nil {
		reader.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rows.Close()
		reader.Close()
	})
	if !rows.Next() {
		t.Fatal("missing fixture row")
	}
	closed := make(chan error, 1)
	go func() { closed <- reader.Close() }()

	// With the sole connection held by rows, this call can only finish once
	// Close shuts the pool, not by acquiring a connection ahead of Close.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if extra, err := reader.QueryContext(ctx, `SELECT 1`); extra != nil || !errors.Is(err, ErrDatabaseUnavailable) {
		if extra != nil {
			extra.Close()
		}
		t.Fatalf("closing pool admitted a query: %v", err)
	}
	duplicate, err := OpenReadOnlyDatabase(context.Background(), path)
	if duplicate != nil {
		duplicate.Close()
		t.Fatal("Close released reservation while SQL rows were live")
	}
	if !errors.Is(err, ErrDatabaseBusy) {
		t.Fatalf("closing reader no longer reserves its inode: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Close did not finish after rows closed")
	}
	reopened, err := OpenReadOnlyDatabase(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func initializeConnectionFixture(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	CreateModsTable(db)
	_, insertErr := db.Exec(`INSERT INTO mods VALUES (1, 'Plugin', 'Owner', '', '1.0.0', NULL, NULL, 0)`)
	if err := errors.Join(insertErr, db.Close()); err != nil {
		t.Fatal(err)
	}
}
