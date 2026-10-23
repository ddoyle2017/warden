package repo_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ddoyle2017/warden/internal/data/repo"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"github.com/mattn/go-sqlite3"
)

func TestListModsContextPreservesFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		queryFailure bool
		mapping      bool
		iteration    bool
		closeFailure bool
		cancel       bool
		class        error
	}{
		"given a query failure, when listing, then preserve its cause": {
			queryFailure: true, class: repo.ErrDatabaseUnavailable,
		},
		"given a malformed later row, when listing, then discard partial results and preserve its conversion cause": {
			mapping: true, class: repo.ErrDatabaseUnsupported,
		},
		"given a later iteration failure, when listing, then discard partial results and close rows": {
			iteration: true, class: repo.ErrDatabaseUnavailable,
		},
		"given a row-close failure, when listing, then do not return successful inventory": {
			closeFailure: true, class: repo.ErrDatabaseUnavailable,
		},
		"given mapping and row-close failures, when listing, then preserve both causes": {
			mapping: true, closeFailure: true, class: repo.ErrDatabaseUnsupported,
		},
		"given cancellation during iteration, when listing, then retain cancellation and close rows": {
			cancel: true, class: repo.ErrDatabaseCancelled,
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			queryErr := errors.New("owned query failure")
			iterationErr := errors.New("owned iteration failure")
			closeErr := errors.New("owned rows close failure")
			connector := &listFailureConnector{
				path: filepath.Join(t.TempDir(), "warden.db"), sqlite: &sqlite3.SQLiteDriver{},
			}
			db := sql.OpenDB(connector)
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			repo.CreateModsTable(db)
			if _, err := db.Exec(`INSERT INTO mods VALUES
				(1, 'First', 'Owner', '', '1.0.0', NULL, NULL, 0),
				(2, 'Second', 'Owner', '', '1.0.0', NULL, NULL, 0)`); err != nil {
				t.Fatal(err)
			}
			if tc.iteration {
				connector.iterationErr = iterationErr
			}
			if tc.closeFailure {
				connector.closeErr = closeErr
			}
			if tc.cancel {
				connector.cancel = cancel
			}
			records := repo.NewModsRepo(&mock.Database{
				QueryContextFunc: func(gotCtx context.Context, query string, args ...any) (*sql.Rows, error) {
					if gotCtx != ctx {
						t.Fatal("listing did not forward the caller's context")
					}
					if tc.queryFailure {
						return nil, queryErr
					}
					if tc.mapping {
						query = `SELECT id, name, namespace, filePath, version, websiteUrl, description, frameworkId FROM mods WHERE id = 1
							UNION ALL SELECT 'broken-id', name, namespace, filePath, version, websiteUrl, description, frameworkId FROM mods WHERE id = 2`
					}
					return db.QueryContext(gotCtx, query, args...)
				},
			})
			mods, err := records.ListModsContext(ctx)
			want := repo.ErrModMappingFailed
			if tc.queryFailure {
				want = repo.ErrModListFailed
			}
			if mods != nil || !errors.Is(err, want) || !errors.Is(err, tc.class) {
				t.Fatalf("failed listing = %+v, %v, want %v and %v", mods, err, want, tc.class)
			}
			for cause, required := range map[error]bool{
				queryErr: tc.queryFailure, iterationErr: tc.iteration, closeErr: tc.closeFailure, context.Canceled: tc.cancel,
			} {
				if required && !errors.Is(err, cause) {
					t.Errorf("missing cause %v: %v", cause, err)
				}
			}
			if tc.mapping {
				var scanError interface {
					error
					Unwrap() error
				}
				if !errors.As(err, &scanError) || !strings.Contains(scanError.Error(), "broken-id") {
					t.Errorf("wrapped scan diagnostic lost: %v", err)
				}
			}
			if !tc.queryFailure && connector.rowsClosed != 1 {
				t.Errorf("rows closed %d times, want 1", connector.rowsClosed)
			}
			if db.Stats().InUse != 0 {
				t.Errorf("listing leaked a connection: %+v", db.Stats())
			}
		})
	}
}

type listFailureConnector struct {
	path         string
	sqlite       *sqlite3.SQLiteDriver
	iterationErr error
	closeErr     error
	cancel       context.CancelFunc
	rowsClosed   int
}

func (c *listFailureConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.sqlite.Open(c.path)
	if err != nil {
		return nil, err
	}
	return &listFailureConnection{SQLiteConn: conn.(*sqlite3.SQLiteConn), connector: c}, nil
}

func (c *listFailureConnector) Driver() driver.Driver { return c.sqlite }

type listFailureConnection struct {
	*sqlite3.SQLiteConn
	connector *listFailureConnector
}

func (c *listFailureConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := c.SQLiteConn.QueryContext(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return &listFailureRows{Rows: rows, connector: c.connector}, nil
}

type listFailureRows struct {
	driver.Rows
	connector *listFailureConnector
	nextCalls int
}

func (r *listFailureRows) Next(dest []driver.Value) error {
	r.nextCalls++
	if r.nextCalls == 2 {
		if r.connector.cancel != nil {
			r.connector.cancel()
		}
		if r.connector.iterationErr != nil {
			return r.connector.iterationErr
		}
	}
	return r.Rows.Next(dest)
}

func (r *listFailureRows) Close() error {
	r.connector.rowsClosed++
	return errors.Join(r.Rows.Close(), r.connector.closeErr)
}
