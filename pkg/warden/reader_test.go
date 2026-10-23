package warden_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ddoyle2017/warden/internal/data/repo"
	"github.com/ddoyle2017/warden/pkg/warden"
)

func TestListInstalledMods(t *testing.T) {
	populated := []warden.InstalledMod{
		{Package: warden.PackageID{Namespace: "Zulu", Name: "First"}, Version: "nightly", InstallPath: "missing"},
		{Package: warden.PackageID{Namespace: "Alpha", Name: "Shared"}, Version: "2.0.0", InstallPath: "z"},
		{Package: warden.PackageID{Namespace: "Alpha", Name: "Shared"}, Version: "10.0.0", InstallPath: "z"},
		{Package: warden.PackageID{Namespace: "Alpha", Name: "Shared"}, Version: "10.0.0", InstallPath: "a"},
		{Package: warden.PackageID{Namespace: "Alpha", Name: "Shared"}, Version: "10.0.0", InstallPath: "a"},
		{Package: warden.PackageID{Namespace: "Alpha", Name: "First"}, Version: " v1? ", InstallPath: "../not-inspected"},
		{Package: warden.PackageID{}, Version: "", InstallPath: ""},
	}
	for name, tc := range map[string]struct {
		filename string
		records  []warden.InstalledMod
		want     []warden.InstalledMod
	}{
		"given only a framework record, when listing, then return a non-nil empty inventory": {
			want: []warden.InstalledMod{},
		},
		"given unordered duplicate and non-release values, when listing, then preserve and sort all mod records": {
			records: populated,
			want:    []warden.InstalledMod{populated[6], populated[5], populated[3], populated[4], populated[2], populated[1], populated[0]},
		},
		"given connection metacharacters in a filename, when listing, then read that literal filesystem path": {
			filename: "warden ?#%+&_query_only=0&mode=rwc.db",
			records:  populated[:1], want: populated[:1],
		},
	} {
		t.Run(name, func(t *testing.T) {
			filename := tc.filename
			if filename == "" {
				filename = "warden.db"
			}
			path := filepath.Join(t.TempDir(), filename)
			createDatabase(t, path, tc.records)
			reader := openReader(t, path)
			got, err := reader.ListInstalledMods(context.Background())
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("inventory = %#v, %v; want %#v", got, err, tc.want)
			}
			if len(got) != 0 {
				got[0] = warden.InstalledMod{Package: warden.PackageID{Namespace: "caller", Name: "changed"}}
			}
			again, err := reader.ListInstalledMods(context.Background())
			if err != nil || !reflect.DeepEqual(again, tc.want) {
				t.Errorf("caller mutation changed later inventory: %#v, %v", again, err)
			}
		})
	}
}

func TestOpenInvalidOptions(t *testing.T) {
	for name, path := range map[string]string{
		"no explicit path":          "",
		"an in-memory database":     ":memory:",
		"a SQLite URI":              "file:warden.db?mode=ro",
		"a writable connection URI": "file:warden.db?mode=rwc",
		"a shared in-memory URI":    "file::memory:?cache=shared",
		"a NUL in the path":         "warden\x00.db",
	} {
		t.Run("given "+name+", when opening, then reject invalid options without a reader", func(t *testing.T) {
			reader, err := warden.Open(context.Background(), warden.Options{DatabasePath: path})
			requireFailedOpen(t, reader, err, warden.ErrInvalidOptions, os.ErrInvalid)
		})
	}
}

func TestOpenStateFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(*testing.T, string) string
		class error
		cause error
	}{
		"missing database": {class: warden.ErrDatabaseUnavailable, cause: os.ErrNotExist},
		"missing parent": {
			setup: func(t *testing.T, path string) string { return filepath.Join(path, "missing", "db") },
			class: warden.ErrDatabaseUnavailable, cause: os.ErrNotExist,
		},
		"directory instead of a database": {
			setup: func(t *testing.T, path string) string { return filepath.Dir(path) },
			class: warden.ErrDatabaseUnsupported, cause: os.ErrInvalid,
		},
		"truncated SQLite header": {
			setup: func(t *testing.T, path string) string {
				writeFile(t, path, []byte("SQLite format 3\x00"))
				return path
			},
			class: warden.ErrDatabaseUnsupported, cause: io.ErrUnexpectedEOF,
		},
		"missing mods table": {
			setup: func(t *testing.T, path string) string {
				createDatabase(t, path, nil, "DROP TABLE mods")
				return path
			},
			class: warden.ErrDatabaseUnsupported, cause: sql.ErrNoRows,
		},
		"incompatible empty schema": {
			setup: func(t *testing.T, path string) string {
				createDatabase(t, path, nil, "DROP TABLE mods", "CREATE TABLE mods (id INTEGER)")
				return path
			},
			class: warden.ErrDatabaseUnsupported,
		},
		"WAL database": {
			setup: func(t *testing.T, path string) string {
				createDatabase(t, path, nil, "PRAGMA journal_mode = WAL")
				return path
			},
			class: warden.ErrDatabaseUnsupported,
		},
	} {
		t.Run("given a "+name+", when opening, then preserve the state classification and cause", func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "warden.db")
			if tc.setup != nil {
				path = tc.setup(t, path)
			}
			before := snapshotTree(t, dir)
			for attempt := 0; attempt < 2; attempt++ {
				reader, err := warden.Open(context.Background(), warden.Options{DatabasePath: path})
				requireFailedOpen(t, reader, err, tc.class, tc.cause)
				if tc.class == warden.ErrDatabaseUnavailable {
					var cause *os.PathError
					if !errors.As(err, &cause) || cause.Path != path {
						t.Errorf("filesystem cause lost: %v", err)
					}
				}
				assertTree(t, dir, before)
			}
		})
	}
}

func TestReaderCancellation(t *testing.T) {
	for name, makeContext := range map[string]func() (context.Context, context.CancelFunc){
		"cancellation": func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, cancel
		},
		"deadline expiry": func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(context.Background(), time.Unix(1, 0))
		},
	} {
		t.Run("given "+name+", when opening or listing, then preserve the context cause and no results", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "warden.db")
			want := []warden.InstalledMod{{Package: warden.PackageID{Namespace: "Owner", Name: "Plugin"}, Version: "1.0.0"}}
			createDatabase(t, path, want)
			ctx, cancel := makeContext()
			defer cancel()
			reader, err := warden.Open(ctx, warden.Options{DatabasePath: path})
			requireFailedOpen(t, reader, err, ctx.Err(), repo.ErrDatabaseCancelled)
			reader = openReader(t, path)
			got, err := reader.ListInstalledMods(ctx)
			if got != nil || !errors.Is(err, ctx.Err()) || !errors.Is(err, repo.ErrDatabaseCancelled) {
				t.Fatalf("cancelled inventory = %#v, %v", got, err)
			}
			got, err = reader.ListInstalledMods(context.Background())
			if err != nil || !slices.Equal(got, want) {
				t.Errorf("read after cancellation = %#v, %v", got, err)
			}
		})
	}
}

func TestReaderOwnershipAndClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.db")
	createDatabase(t, path, nil)
	symlink, hardlink := filepath.Join(dir, "symlink.db"), filepath.Join(dir, "hardlink.db")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, hardlink); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, path)
	if err != nil {
		t.Fatal(err)
	}
	reader := openReader(t, path)
	aliases := []string{path, relative, symlink, hardlink}
	for _, alias := range aliases {
		duplicate, err := warden.Open(context.Background(), warden.Options{DatabasePath: alias})
		requireFailedOpen(t, duplicate, err, warden.ErrDatabaseBusy, repo.ErrDatabaseBusy)
		got, err := reader.ListInstalledMods(context.Background())
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("original reader after duplicate open = %#v, %v", got, err)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
		got, err := reader.ListInstalledMods(context.Background())
		if got != nil || !errors.Is(err, warden.ErrClosed) {
			t.Errorf("closed inventory = %#v, %v", got, err)
		}
	}
	for _, alias := range aliases {
		reopened := openReader(t, alias)
		got, err := reopened.ListInstalledMods(context.Background())
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("reopened inventory = %#v, %v", got, err)
		}
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReaderSharedCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warden.db")
	want := []warden.InstalledMod{{Package: warden.PackageID{Namespace: "Owner", Name: "Plugin"}, Version: "1.0.0"}}
	createDatabase(t, path, want)
	reader := openReader(t, path)
	var workers sync.WaitGroup
	start := make(chan struct{})
	for worker := 0; worker < 16; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for call := 0; call < 16; call++ {
				got, err := reader.ListInstalledMods(context.Background())
				if err != nil || !slices.Equal(got, want) {
					t.Errorf("concurrent inventory = %#v, %v", got, err)
					return
				}
				got[0].Package.Name = "caller-owned"
			}
		}()
	}
	close(start)
	workers.Wait()
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openReader(t, path)
	got, err := reopened.ListInstalledMods(context.Background())
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("reopened after concurrent reads = %#v, %v", got, err)
	}
}

func TestReaderRejectsPartialInventory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warden.db")
	createDatabase(t, path, []warden.InstalledMod{{Package: warden.PackageID{Name: "Valid"}}},
		`INSERT INTO mods (name, namespace, filePath, version, frameworkId) VALUES
			('Invalid', 'Owner', '', '', 'invalid integer')`)
	reader := openReader(t, path)
	got, err := reader.ListInstalledMods(context.Background())
	if got != nil || !errors.Is(err, warden.ErrDatabaseUnsupported) ||
		!errors.Is(err, repo.ErrModMappingFailed) || !errors.Is(err, repo.ErrDatabaseUnsupported) {
		t.Fatalf("malformed inventory = %#v, %v", got, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openReader(t, path)
	got, err = reopened.ListInstalledMods(context.Background())
	if got != nil || !errors.Is(err, warden.ErrDatabaseUnsupported) {
		t.Errorf("reopened malformed inventory = %#v, %v", got, err)
	}
}

func TestZeroReader(t *testing.T) {
	var reader warden.Reader
	got, err := reader.ListInstalledMods(context.Background())
	if got != nil || !errors.Is(err, warden.ErrClosed) {
		t.Errorf("zero reader inventory = %#v, %v", got, err)
	}
	if err := reader.Close(); err != nil {
		t.Errorf("zero reader close = %v", err)
	}
}

func createDatabase(t *testing.T, path string, records []warden.InstalledMod, statements ...string) {
	t.Helper()
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=rwc&_busy_timeout=0"}
	db, err := sql.Open("sqlite3", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	repo.CreateFrameworksTable(db)
	repo.CreateModsTable(db)
	if _, err := db.Exec(`INSERT INTO frameworks (name, namespace, version) VALUES ('BepInEx', 'FrameworkOwner', '5.0.0')`); err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if _, err := db.Exec(`INSERT INTO mods (name, namespace, filePath, version, frameworkId)
			VALUES (?, ?, ?, ?, 1)`, record.Package.Name, record.Package.Namespace, record.InstallPath, record.Version); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func openReader(t *testing.T, path string) *warden.Reader {
	t.Helper()
	reader, err := warden.Open(context.Background(), warden.Options{DatabasePath: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	})
	return reader
}

func requireFailedOpen(t *testing.T, reader *warden.Reader, err, class, cause error) {
	t.Helper()
	if reader != nil {
		if closeErr := reader.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal("failed open returned a reader")
	}
	if !errors.Is(err, class) || (cause != nil && !errors.Is(err, cause)) {
		t.Fatalf("open = %v; want %v with %v", err, class, cause)
	}
}
