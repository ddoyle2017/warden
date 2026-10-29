package repo_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ddoyle2017/warden/internal/data/repo"
	"github.com/ddoyle2017/warden/internal/domain/mod"
	"github.com/mattn/go-sqlite3"
)

func TestReadOnlyInventory(t *testing.T) {
	for name, tc := range map[string]struct {
		filename string
		schema   string
		insert   string
		want     []mod.Mod
	}{
		"given an empty mods table, when reading, then return an empty inventory without writing": {},
		"given optional NULL values and duplicate identities, when reading, then preserve every record": {
			insert: `INSERT INTO mods (name, namespace, filePath, version, frameworkId) VALUES
				('Shared', 'Owner', '/not-inspected/one', '1.0.0', 0),
				('Shared', 'Other', '/not-inspected/two', '2.0.0', 4),
				('Shared', 'Owner', '/not-inspected/three', '0.9.0', 0)`,
			want: []mod.Mod{
				{ID: 1, Name: "Shared", Namespace: "Owner", FilePath: "/not-inspected/one", Version: "1.0.0"},
				{ID: 2, Name: "Shared", Namespace: "Other", FilePath: "/not-inspected/two", Version: "2.0.0", FrameworkID: 4},
				{ID: 3, Name: "Shared", Namespace: "Owner", FilePath: "/not-inspected/three", Version: "0.9.0"},
			},
		},
		"given reordered and additive compatible columns, when reading, then map only consumed metadata": {
			schema: `CREATE TABLE mods (extra BLOB, description TEXT, namespace VARCHAR(50) NOT NULL,
				id INTEGER PRIMARY KEY, version TEXT NOT NULL, name TEXT NOT NULL,
				websiteUrl TEXT, frameworkId INT NOT NULL, filePath TEXT NOT NULL)`,
			insert: `INSERT INTO mods VALUES (X'0102', 'description', 'Owner', 42, '3.0.0', 'Plugin',
				'https://example.invalid', 7, 'unread-path')`,
			want: []mod.Mod{{ID: 42, FrameworkID: 7, Name: "Plugin", Namespace: "Owner", Version: "3.0.0",
				WebsiteURL: "https://example.invalid", Description: "description", FilePath: "unread-path"}},
		},
		"given URI metacharacters in a filesystem path, when opening, then use that literal file": {
			filename: "warden ?#%+&_query_only=0&mode=rwc-\u00e9.db",
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			filename := tc.filename
			if filename == "" {
				filename = "warden.db"
			}
			path := filepath.Join(dir, filename)
			createInventoryDatabase(t, path, tc.schema, tc.insert)
			before := snapshotDatabaseTree(t, dir)
			db := openReadOnly(t, path)
			assertDatabaseTree(t, dir, before)

			got, err := repo.NewModsRepo(db).ListModsContext(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !slices.EqualFunc(got, tc.want, func(a, b mod.Mod) bool { return a.Equals(&b) }) {
				t.Errorf("inventory = %+v, want %+v", got, tc.want)
			}
			assertDatabaseTree(t, dir, before)
			for pragma, want := range map[string]string{
				"query_only": "1", "busy_timeout": "0", "journal_mode": "delete", "locking_mode": "normal",
			} {
				rows, err := db.Query("PRAGMA " + pragma)
				if err != nil {
					t.Fatal(err)
				}
				var value string
				if !rows.Next() {
					t.Fatal("missing PRAGMA value")
				}
				if err := rows.Scan(&value); err != nil {
					t.Fatal(err)
				}
				if err := rows.Close(); err != nil {
					t.Fatal(err)
				}
				if value != want {
					t.Errorf("%s = %q, want %q", pragma, value, want)
				}
			}
			for _, query := range []string{
				`CREATE TABLE forbidden (id INTEGER)`,
				`CREATE TEMP TABLE forbidden (id INTEGER)`,
				`INSERT INTO mods (name, namespace, filePath, version, frameworkId) VALUES ('x', 'y', '', '', 0)`,
			} {
				assertReadOnlyWriteRejected(t, db, query)
				assertDatabaseTree(t, dir, before)
			}
			// mode=ro must still protect the database independently of query_only.
			execReadOnlyStatement(t, db, `PRAGMA query_only = OFF`)
			assertReadOnlyWriteRejected(t, db, `DELETE FROM mods`)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			assertDatabaseTree(t, dir, before)
			if err := db.Close(); err != nil {
				t.Errorf("second close: %v", err)
			}
		})
	}
}

func TestReadOnlyOpenFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(*testing.T, string) string
		class error
		cause error
	}{
		"given a missing file, when opening, then do not create it": {
			class: repo.ErrDatabaseUnavailable, cause: os.ErrNotExist,
		},
		"given a missing parent, when opening, then do not create directories": {
			setup: func(t *testing.T, path string) string { return filepath.Join(path, "missing", "db") },
			class: repo.ErrDatabaseUnavailable, cause: os.ErrNotExist,
		},
		"given a regular file as parent, when opening, then preserve the filesystem error": {
			setup: func(t *testing.T, path string) string {
				writeFixtureFile(t, path, []byte("not a directory"))
				return filepath.Join(path, "db")
			},
			class: repo.ErrDatabaseUnavailable,
		},
		"given a directory, when opening, then reject a non-regular database": {
			setup: func(t *testing.T, path string) string { return filepath.Dir(path) },
			class: repo.ErrDatabaseUnsupported, cause: os.ErrInvalid,
		},
		"given an empty file and stale journal, when opening, then reject before SQLite can delete the journal": {
			setup: func(t *testing.T, path string) string {
				writeFixtureFile(t, path, nil)
				writeFixtureFile(t, path+"-journal", []byte("retained stale journal"))
				return path
			},
			class: repo.ErrDatabaseUnsupported, cause: io.EOF,
		},
		"given a truncated header, when opening, then retain the short-read cause": {
			setup: func(t *testing.T, path string) string {
				writeFixtureFile(t, path, []byte("SQLite format 3\x00"))
				return path
			},
			class: repo.ErrDatabaseUnsupported, cause: io.ErrUnexpectedEOF,
		},
		"given a non-SQLite file, when opening, then reject without modifying it": {
			setup: func(t *testing.T, path string) string {
				writeFixtureFile(t, path, bytes.Repeat([]byte("x"), 100))
				return path
			},
			class: repo.ErrDatabaseUnsupported,
		},
		"given no mods table, when opening, then reject even without records": {
			setup: func(t *testing.T, path string) string {
				createInventoryDatabase(t, path, `CREATE TABLE unrelated (id INTEGER)`, "")
				return path
			},
			class: repo.ErrDatabaseUnsupported, cause: sql.ErrNoRows,
		},
		"given an empty incompatible table, when opening, then reject its schema": {
			setup: func(t *testing.T, path string) string {
				createInventoryDatabase(t, path, `CREATE TABLE mods (id INTEGER)`, "")
				return path
			},
			class: repo.ErrDatabaseUnsupported,
		},
		"given wrong declared types, when opening, then reject even an empty inventory": {
			setup: func(t *testing.T, path string) string {
				createInventoryDatabase(t, path, "", `ALTER TABLE mods RENAME COLUMN version TO old_version; ALTER TABLE mods ADD COLUMN version INTEGER NOT NULL DEFAULT 0`)
				return path
			},
			class: repo.ErrDatabaseUnsupported,
		},
		"given nullable required columns, when opening, then reject even an empty inventory": {
			setup: func(t *testing.T, path string) string {
				createInventoryDatabase(t, path, "", `ALTER TABLE mods RENAME TO original; CREATE TABLE mods AS SELECT * FROM original`)
				return path
			},
			class: repo.ErrDatabaseUnsupported,
		},
		"given a nullable descending primary key, when opening, then do not mistake it for a rowid alias": {
			setup: func(t *testing.T, path string) string {
				createInventoryDatabase(t, path, `CREATE TABLE mods (
					id INTEGER PRIMARY KEY DESC, name TEXT NOT NULL, namespace TEXT NOT NULL,
					filePath TEXT NOT NULL, version TEXT NOT NULL, websiteUrl TEXT,
					description TEXT, frameworkId INTEGER NOT NULL)`, "")
				return path
			},
			class: repo.ErrDatabaseUnsupported,
		},
		"given a view named mods, when opening, then reject the unsupported object": {
			setup: func(t *testing.T, path string) string {
				createInventoryDatabase(t, path, `CREATE TABLE original (id INTEGER); CREATE VIEW mods AS SELECT * FROM original`, "")
				return path
			},
			class: repo.ErrDatabaseUnsupported,
		},
		"given no explicit path, when opening, then reject implicit storage": {
			setup: func(t *testing.T, path string) string { return "" },
			class: repo.ErrDatabaseUnsupported, cause: os.ErrInvalid,
		},
		"given an in-memory DSN, when opening, then reject non-filesystem storage": {
			setup: func(t *testing.T, path string) string { return ":memory:" },
			class: repo.ErrDatabaseUnsupported, cause: os.ErrInvalid,
		},
		"given a SQLite URI, when opening, then reject caller connection options": {
			setup: func(t *testing.T, path string) string { return "file:" + path + "?mode=rwc" },
			class: repo.ErrDatabaseUnsupported, cause: os.ErrInvalid,
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "warden.db")
			if tc.setup != nil {
				path = tc.setup(t, path)
			}
			before := snapshotDatabaseTree(t, dir)
			for attempt := 0; attempt < 2; attempt++ {
				db, err := repo.OpenReadOnlyDatabase(context.Background(), path)
				if db != nil {
					db.Close()
					t.Fatal("failed open returned a handle")
				}
				if !errors.Is(err, tc.class) || (tc.cause != nil && !errors.Is(err, tc.cause)) {
					t.Fatalf("open = %v, want %v with %v", err, tc.class, tc.cause)
				}
				if errors.Is(tc.class, repo.ErrDatabaseUnavailable) {
					var cause *os.PathError
					if !errors.As(err, &cause) {
						t.Errorf("filesystem cause lost: %v", err)
					}
				}
				assertDatabaseTree(t, dir, before)
			}
		})
	}
}

func TestReadOnlyMalformedHeaders(t *testing.T) {
	for name, change := range map[string]func([]byte){
		"unsupported read version":  func(b []byte) { b[19] = 3 },
		"unsupported write version": func(b []byte) { b[18] = 3 },
		"invalid page size":         func(b []byte) { b[16], b[17] = 0, 3 },
		"invalid payload fraction":  func(b []byte) { b[21] = 0 },
		"unsupported schema format": func(b []byte) { b[47] = 5 },
		"unsupported encoding":      func(b []byte) { b[59] = 4 },
	} {
		t.Run("given "+name+", when opening, then reject before SQLite access", func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "warden.db")
			createInventoryDatabase(t, path, "", "")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			change(data)
			writeFixtureFile(t, path, data)
			before := snapshotDatabaseTree(t, dir)
			db, err := repo.OpenReadOnlyDatabase(context.Background(), path)
			if db != nil {
				db.Close()
				t.Fatal("malformed header returned a handle")
			}
			if !errors.Is(err, repo.ErrDatabaseUnsupported) {
				t.Errorf("open = %v", err)
			}
			assertDatabaseTree(t, dir, before)
		})
	}
}

func TestReadOnlyCancellationAndConcurrentCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.db")
	createInventoryDatabase(t, path, "", `INSERT INTO mods VALUES (1, 'Plugin', 'Owner', '', '1.0.0', NULL, NULL, 0)`)
	before := snapshotDatabaseTree(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if db, err := repo.OpenReadOnlyDatabase(ctx, path); db != nil ||
		!errors.Is(err, context.Canceled) || !errors.Is(err, repo.ErrDatabaseCancelled) {
		if db != nil {
			db.Close()
		}
		t.Fatalf("cancelled open = %v, %v", db, err)
	}
	db := openReadOnly(t, path)
	mods := repo.NewModsRepo(db)
	for _, ctx := range []context.Context{ctx, expiredContext(t)} {
		got, err := mods.ListModsContext(ctx)
		if got != nil || !errors.Is(err, ctx.Err()) || !errors.Is(err, repo.ErrDatabaseCancelled) {
			t.Errorf("cancelled list = %+v, %v", got, err)
		}
		if errors.Is(err, repo.ErrDatabaseUnsupported) {
			t.Errorf("cancellation was also classified as unsupported storage: %v", err)
		}
	}
	var workers sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			got, err := mods.ListModsContext(context.Background())
			if err != nil || len(got) != 1 || got[0].Name != "Plugin" || got[0].Namespace != "Owner" {
				t.Errorf("concurrent inventory = %+v, %v", got, err)
			}
		}()
	}
	close(start)
	workers.Wait()
	assertDatabaseTree(t, dir, before)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := mods.ListModsContext(context.Background())
	if got != nil || !errors.Is(err, repo.ErrDatabaseUnavailable) {
		t.Errorf("closed list = %+v, %v", got, err)
	}
	assertDatabaseTree(t, dir, before)
}

func TestReadOnlyRejectsWAL(t *testing.T) {
	for _, sidecars := range []bool{false, true} {
		name := "given WAL without sidecars, when opening, then do not create sidecars"
		if sidecars {
			name = "given WAL with live sidecars, when opening, then leave every file untouched"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "warden.db")
			createInventoryDatabase(t, path, "", "")
			writer := startSQLiteWriter(t, path)
			writer.mustExec(`PRAGMA journal_mode = WAL`)
			writer.mustExec(`INSERT INTO mods VALUES (1, 'Plugin', 'Owner', '', '1.0.0', NULL, NULL, 0)`)
			if !sidecars {
				writer.stop()
			}
			for _, suffix := range []string{"-wal", "-shm"} {
				_, err := os.Stat(path + suffix)
				if sidecars && err != nil || !sidecars && !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("invalid WAL fixture %s: %v", suffix, err)
				}
			}
			before := snapshotDatabaseTree(t, dir)
			if before["."].mode.Perm()&0200 == 0 {
				t.Fatal("WAL rejection must be exercised in a writable directory")
			}
			db, err := repo.OpenReadOnlyDatabase(context.Background(), path)
			if db != nil {
				db.Close()
				t.Fatal("WAL open returned a handle")
			}
			if !errors.Is(err, repo.ErrDatabaseUnsupported) {
				t.Fatalf("WAL open = %v", err)
			}
			var sqliteErr sqlite3.Error
			if errors.As(err, &sqliteErr) {
				t.Errorf("WAL reached SQLite instead of the preflight: %v", err)
			}
			assertDatabaseTree(t, dir, before)
		})
	}
}

func createInventoryDatabase(t *testing.T, path, schema, insert string) {
	t.Helper()
	db := openFixtureDatabase(t, path)
	if schema == "" {
		repo.CreateModsTable(db)
	} else if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if insert != "" {
		if _, err := db.Exec(insert); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func openFixtureDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=rwc&_busy_timeout=0"}
	db, err := sql.Open("sqlite3", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func openReadOnly(t *testing.T, path string) *repo.ReadOnlyDatabase {
	t.Helper()
	db, err := repo.OpenReadOnlyDatabase(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func execReadOnlyStatement(t *testing.T, db *repo.ReadOnlyDatabase, query string) {
	t.Helper()
	statement, err := db.Prepare(query)
	if err != nil {
		t.Fatal(err)
	}
	_, execErr := statement.Exec()
	if err := errors.Join(execErr, statement.Close()); err != nil {
		t.Fatal(err)
	}
}

func assertReadOnlyWriteRejected(t *testing.T, db *repo.ReadOnlyDatabase, query string) {
	t.Helper()
	statement, err := db.Prepare(query)
	if err == nil {
		_, err = statement.Exec()
		err = errors.Join(err, statement.Close())
	}
	var sqliteErr sqlite3.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code != sqlite3.ErrReadonly {
		t.Errorf("write %q = %v, want SQLITE_READONLY", query, err)
	}
}

func expiredContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	t.Cleanup(cancel)
	return ctx
}

func writeFixtureFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

type databaseFileSnapshot struct {
	info     os.FileInfo
	mode     os.FileMode
	modified time.Time
	contents []byte
}

func snapshotDatabaseTree(t *testing.T, dir string) map[string]databaseFileSnapshot {
	t.Helper()
	snapshot := make(map[string]databaseFileSnapshot)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		item := databaseFileSnapshot{info: info, mode: info.Mode(), modified: info.ModTime()}
		if info.Mode().IsRegular() {
			item.contents, err = os.ReadFile(path)
		}
		snapshot[relative] = item
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertDatabaseTree(t *testing.T, dir string, before map[string]databaseFileSnapshot) {
	t.Helper()
	after := snapshotDatabaseTree(t, dir)
	if len(after) != len(before) {
		t.Errorf("filesystem entries changed: before %v, after %v", reflect.ValueOf(before).MapKeys(), reflect.ValueOf(after).MapKeys())
	}
	for path, want := range before {
		got, ok := after[path]
		if !ok || !os.SameFile(want.info, got.info) || want.mode != got.mode ||
			!want.modified.Equal(got.modified) || !bytes.Equal(want.contents, got.contents) {
			t.Errorf("reader changed %q (entry, identity, mode, modification time, or contents)", path)
		}
	}
}

func assertInventoryVersion(t *testing.T, db *repo.ReadOnlyDatabase, want string) {
	t.Helper()
	mods, err := repo.NewModsRepo(db).ListModsContext(context.Background())
	if err != nil || len(mods) != 1 || mods[0].Version != want {
		t.Fatalf("inventory = %+v, %v, want version %s", mods, err, want)
	}
}

func TestReadOnlyRecoveryRequired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.db")
	createInventoryDatabase(t, path, "", `INSERT INTO mods VALUES (1, 'Plugin', 'Owner', '', '1.0.0', NULL, NULL, 0)`)
	reader := openReadOnly(t, path)
	assertInventoryVersion(t, reader, "1.0.0")
	writer := startSQLiteWriter(t, path)
	writer.mustExec(`PRAGMA cache_size = 2`)
	writer.mustExec(`BEGIN IMMEDIATE`)
	writer.mustExec(`UPDATE mods SET version = 'uncommitted'`)
	writer.mustExec(`WITH RECURSIVE n(i) AS (VALUES(2) UNION ALL SELECT i+1 FROM n WHERE i < 100)
		INSERT INTO mods SELECT i, 'Plugin', 'Owner', '', 'uncommitted', NULL, printf('%04000d', i), 0 FROM n`)
	writer.crash()
	journal, err := os.ReadFile(path + "-journal")
	if err != nil || len(journal) < 512 || !bytes.Equal(journal[:8], []byte{0xd9, 0xd5, 0x05, 0xf9, 0x20, 0xa1, 0x63, 0xd7}) {
		t.Fatalf("fixture did not produce a synced hot journal: size=%d, error=%v", len(journal), err)
	}
	before := snapshotDatabaseTree(t, dir)
	mods, err := repo.NewModsRepo(reader).ListModsContext(context.Background())
	var sqliteErr sqlite3.Error
	if mods != nil || !errors.Is(err, repo.ErrDatabaseRecoveryRequired) || !errors.As(err, &sqliteErr) ||
		sqliteErr.ExtendedCode != sqlite3.ErrReadonlyRollback {
		t.Fatalf("recovery-required read = %+v, %v", mods, err)
	}
	assertDatabaseTree(t, dir, before)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	assertDatabaseTree(t, dir, before)
	db, err := repo.OpenReadOnlyDatabase(context.Background(), path)
	if db != nil {
		db.Close()
		t.Fatal("hot-journal open returned a handle")
	}
	if !errors.Is(err, repo.ErrDatabaseRecoveryRequired) || !errors.As(err, &sqliteErr) ||
		sqliteErr.ExtendedCode != sqlite3.ErrReadonlyRollback {
		t.Fatalf("recovery-required open = %v", err)
	}
	assertDatabaseTree(t, dir, before)

	// A writable owner can recover a copy to the last committed inventory,
	// demonstrating that the rejected fixture actually requires rollback.
	recoveredPath := filepath.Join(t.TempDir(), "recovered.db")
	writeFixtureFile(t, recoveredPath, before["warden.db"].contents)
	writeFixtureFile(t, recoveredPath+"-journal", journal)
	recovered := openFixtureDatabase(t, recoveredPath)
	mods, err = repo.NewModsRepo(recovered).ListMods()
	if err != nil || len(mods) != 1 || mods[0].Version != "1.0.0" {
		t.Fatalf("writable recovery = %+v, %v", mods, err)
	}
	if _, err := os.Stat(recoveredPath + "-journal"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("writable owner did not retire the recovered journal: %v", err)
	}
	assertDatabaseTree(t, dir, before)
}

func TestReadOnlyColdJournal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.db")
	createInventoryDatabase(t, path, "", "")
	writer := startSQLiteWriter(t, path)
	writer.mustExec(`PRAGMA journal_mode = PERSIST`)
	writer.mustExec(`INSERT INTO mods VALUES (1, 'Plugin', 'Owner', '', '1.0.0', NULL, NULL, 0)`)
	writer.stop()
	journal, err := os.ReadFile(path + "-journal")
	if err != nil || len(journal) == 0 || journal[0] != 0 {
		t.Fatalf("fixture did not leave a cold rollback journal: %v", err)
	}
	before := snapshotDatabaseTree(t, dir)
	reader := openReadOnly(t, path)
	assertDatabaseTree(t, dir, before)
	assertInventoryVersion(t, reader, "1.0.0")
	assertDatabaseTree(t, dir, before)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	assertDatabaseTree(t, dir, before)
}

func TestReadOnlyLiveWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.db")
	createInventoryDatabase(t, path, "", `INSERT INTO mods VALUES (1, 'Plugin', 'Owner', '', '1.0.0', NULL, NULL, 0)`)
	reader := openReadOnly(t, path)
	writer := startSQLiteWriter(t, path)
	writer.mustExec(`BEGIN IMMEDIATE`)
	writer.mustExec(`UPDATE mods SET version = '2.0.0'`)
	if _, err := os.Stat(path + "-journal"); err != nil {
		t.Fatalf("live writer did not create a rollback journal: %v", err)
	}
	before := snapshotDatabaseTree(t, dir)
	assertInventoryVersion(t, reader, "1.0.0")
	assertDatabaseTree(t, dir, before)
	writer.mustExec(`COMMIT`)
	before = snapshotDatabaseTree(t, dir)
	assertInventoryVersion(t, reader, "2.0.0")
	assertDatabaseTree(t, dir, before)

	writer.mustExec(`BEGIN EXCLUSIVE`)
	writer.mustExec(`UPDATE mods SET version = '3.0.0'`)
	before = snapshotDatabaseTree(t, dir)
	mods, err := repo.NewModsRepo(reader).ListModsContext(context.Background())
	var sqliteErr sqlite3.Error
	if mods != nil || !errors.Is(err, repo.ErrDatabaseBusy) ||
		!errors.As(err, &sqliteErr) || sqliteErr.Code != sqlite3.ErrBusy {
		t.Fatalf("exclusive writer read = %+v, %v", mods, err)
	}
	assertDatabaseTree(t, dir, before)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := repo.OpenReadOnlyDatabase(context.Background(), path)
	if opened != nil {
		opened.Close()
		t.Fatal("exclusive writer allowed reader initialization")
	}
	if !errors.Is(err, repo.ErrDatabaseBusy) || !errors.As(err, &sqliteErr) || sqliteErr.Code != sqlite3.ErrBusy {
		t.Fatalf("exclusive writer open = %v", err)
	}
	assertDatabaseTree(t, dir, before)
	writer.mustExec(`ROLLBACK`)
	reopened := openReadOnly(t, path)
	assertInventoryVersion(t, reopened, "2.0.0")
}

func TestReadOnlyDuplicateAliasesPreserveLocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.db")
	createInventoryDatabase(t, path, "", `INSERT INTO mods VALUES (1, 'Plugin', 'Owner', '', '1.0.0', NULL, NULL, 0)`)
	hardlink, symlink := filepath.Join(dir, "hard.db"), filepath.Join(dir, "sym.db")
	if err := os.Link(path, hardlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	reader := openReadOnly(t, path)
	writer := startSQLiteWriter(t, path)
	rows, err := reader.Query(`SELECT * FROM mods`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rows.Close() })
	if !rows.Next() {
		t.Fatal("reader did not acquire a shared lock")
	}
	writer.mustExec(`BEGIN IMMEDIATE`)
	writer.mustExec(`UPDATE mods SET version = '2.0.0'`)
	for _, alias := range []string{path, hardlink, symlink, filepath.Dir(path) + "/./" + filepath.Base(path)} {
		duplicate, err := repo.OpenReadOnlyDatabase(context.Background(), alias)
		if duplicate != nil {
			duplicate.Close()
			t.Fatal("duplicate open returned a handle")
		}
		if !errors.Is(err, repo.ErrDatabaseBusy) {
			t.Errorf("duplicate open = %v", err)
		}
		if response := writer.execute(`COMMIT`); response.Code != int(sqlite3.ErrBusy) {
			t.Fatalf("duplicate preflight released the reader lock: %+v", response)
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	writer.mustExec(`COMMIT`)
	assertInventoryVersion(t, reader, "2.0.0")
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openReadOnly(t, symlink)
	assertInventoryVersion(t, reopened, "2.0.0")
}

func TestReadOnlyMalformedRowsDoNotReturnPartialInventory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.db")
	createInventoryDatabase(t, path, "", `INSERT INTO mods VALUES
		(1, 'Valid', 'Owner', '', '1.0.0', NULL, NULL, 0),
		(2, 'Broken', 'Owner', '', '1.0.0', NULL, NULL, 'invalid integer')`)
	before := snapshotDatabaseTree(t, dir)
	db := openReadOnly(t, path)
	mods, err := repo.NewModsRepo(db).ListModsContext(context.Background())
	if mods != nil || !errors.Is(err, repo.ErrModMappingFailed) || !errors.Is(err, repo.ErrDatabaseUnsupported) {
		t.Errorf("malformed inventory = %+v, %v", mods, err)
	}
	if err == nil || !strings.Contains(err.Error(), "invalid integer") {
		t.Errorf("mapping cause lost: %v", err)
	}
	assertDatabaseTree(t, dir, before)
}
