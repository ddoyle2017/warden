package repo

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestModScanCancellation(t *testing.T) {
	for name, cause := range map[string]error{
		"cancellation": context.Canceled,
		"deadline":     context.DeadlineExceeded,
	} {
		for row, cancelAt := range map[string]int{"first": 1, "second": 2} {
			t.Run("given "+name+" after the "+row+" Next, when scanning, then return only the cancellation class", func(t *testing.T) {
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

				cancelCtx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				// Deliver both context causes from a controlled Done signal, not a timer.
				ctx := scanCancellationContext{Context: cancelCtx, cause: cause}
				rows, err := reader.QueryContext(ctx, `SELECT id, name, namespace, filePath, version,
					websiteUrl, description, frameworkId FROM mods CROSS JOIN (SELECT 1 UNION ALL SELECT 2)`)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := rows.Close(); err != nil {
						t.Error(err)
					}
				})
				guard, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				tracked := &cancelBeforeScanRows{
					Rows: rows,
					afterNext: func(nextCalls int) {
						if nextCalls != cancelAt {
							return
						}
						cancel()
						// The single connection is available only after database/sql
						// has closed the cancelled rows and stored their context error.
						conn, err := reader.db.Conn(guard)
						if err != nil {
							t.Fatalf("cancelled rows did not release their connection: %v", err)
						}
						if err := conn.Close(); err != nil {
							t.Fatal(err)
						}
					},
				}

				mods, err := mapRowsToMod(tracked)
				if tracked.nextCalls != cancelAt || !errors.Is(tracked.scanError, cause) {
					t.Fatalf("did not reach the intended Scan failure: next=%d, scan=%v", tracked.nextCalls, tracked.scanError)
				}
				if mods != nil || !errors.Is(err, cause) {
					t.Fatalf("cancelled mapping = %+v, %v, want no partial inventory and %v", mods, err, cause)
				}
				classified := classifyDatabaseError(err)
				if !errors.Is(classified, ErrDatabaseCancelled) || errors.Is(classified, ErrDatabaseUnsupported) {
					t.Errorf("Scan cancellation has incorrect storage classification: %v", classified)
				}
				if reader.db.Stats().InUse != 0 {
					t.Errorf("cancelled mapping leaked SQL resources: %+v", reader.db.Stats())
				}
			})
		}
	}
}

type scanCancellationContext struct {
	context.Context
	cause error
}

func (c scanCancellationContext) Err() error {
	if c.Context.Err() != nil {
		return c.cause
	}
	return nil
}

type cancelBeforeScanRows struct {
	*sql.Rows
	afterNext func(int)
	nextCalls int
	scanError error
}

func (r *cancelBeforeScanRows) Next() bool {
	if !r.Rows.Next() {
		return false
	}
	r.nextCalls++
	r.afterNext(r.nextCalls)
	return true
}

func (r *cancelBeforeScanRows) Scan(dest ...any) error {
	err := r.Rows.Scan(dest...)
	if err != nil {
		r.scanError = err
	}
	return err
}
