package warden_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ddoyle2017/warden/pkg/warden"
	"github.com/mattn/go-sqlite3"
)

func TestReaderHasNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.db")
	createDatabase(t, path, []warden.InstalledMod{{
		Package: warden.PackageID{Namespace: "Owner", Name: "Plugin"}, Version: "not-semver", InstallPath: "missing/plugin",
	}})
	for _, name := range []string{"home", "server", "downloads"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(dir, "home", ".warden.yaml"), []byte("invalid YAML: ["))
	writeFile(t, filepath.Join(dir, "server", "keep.txt"), []byte("unrelated server contents"))
	before := snapshotTree(t, dir)
	command := readerProcess(t, path, "side-effects")
	command.Dir = dir
	command.Env = append(command.Env, "HOME="+filepath.Join(dir, "home"), "USERPROFILE="+filepath.Join(dir, "home"))
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("isolated reader: %v\nstdout: %s\nstderr: %s", err, &stdout, &stderr)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("library emitted terminal output: stdout=%q, stderr=%q", &stdout, &stderr)
	}
	assertTree(t, dir, before)
}

func TestReaderBusyWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warden.db")
	want := []warden.InstalledMod{{Package: warden.PackageID{Namespace: "Owner", Name: "Plugin"}}}
	createDatabase(t, path, want)
	reader := openReader(t, path)
	release := holdExclusiveWriter(t, path)
	got, err := reader.ListInstalledMods(context.Background())
	if got != nil {
		t.Fatalf("busy read returned results: %#v", got)
	}
	assertSQLiteError(t, err, warden.ErrDatabaseBusy, sqlite3.ErrBusy.Extend(0))
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	failed, err := warden.Open(context.Background(), warden.Options{DatabasePath: path})
	requireFailedOpen(t, failed, err, warden.ErrDatabaseBusy, nil)
	assertSQLiteError(t, err, warden.ErrDatabaseBusy, sqlite3.ErrBusy.Extend(0))
	release()
	reopened := openReader(t, path)
	got, err = reopened.ListInstalledMods(context.Background())
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("inventory after writer releases lock = %#v, %v", got, err)
	}
}

func TestReaderRecoveryRequired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.db")
	createDatabase(t, path, []warden.InstalledMod{{Package: warden.PackageID{Name: "Original"}, Version: "committed"}})
	reader := openReader(t, path)
	if output, err := readerProcess(t, path, "hot-journal").CombinedOutput(); err != nil {
		t.Fatalf("hot-journal fixture: %v\n%s", err, output)
	}
	journal, err := os.ReadFile(path + "-journal")
	if err != nil || len(journal) < 512 || !bytes.Equal(journal[:8], []byte{0xd9, 0xd5, 0x05, 0xf9, 0x20, 0xa1, 0x63, 0xd7}) {
		t.Fatalf("fixture did not leave a synced hot journal: size=%d, error=%v", len(journal), err)
	}
	before := snapshotTree(t, dir)
	got, err := reader.ListInstalledMods(context.Background())
	if got != nil {
		t.Fatalf("recovery-required read returned results: %#v", got)
	}
	assertSQLiteError(t, err, warden.ErrDatabaseRecoveryRequired, sqlite3.ErrReadonlyRollback)
	assertTree(t, dir, before)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	failed, err := warden.Open(context.Background(), warden.Options{DatabasePath: path})
	requireFailedOpen(t, failed, err, warden.ErrDatabaseRecoveryRequired, nil)
	assertSQLiteError(t, err, warden.ErrDatabaseRecoveryRequired, sqlite3.ErrReadonlyRollback)
	assertTree(t, dir, before)
}

func TestReaderProcess(t *testing.T) {
	mode := os.Getenv("WARDEN_PUBLIC_READER_PROCESS")
	if mode == "" {
		return
	}
	path := os.Getenv("WARDEN_PUBLIC_READER_DATABASE")
	if mode == "side-effects" {
		transport := &rejectNetwork{}
		http.DefaultTransport = transport
		reader := openReader(t, path)
		got, err := reader.ListInstalledMods(context.Background())
		want := []warden.InstalledMod{{
			Package: warden.PackageID{Namespace: "Owner", Name: "Plugin"}, Version: "not-semver", InstallPath: "missing/plugin",
		}}
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("isolated inventory = %#v, %v", got, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
		got, err = reader.ListInstalledMods(context.Background())
		if got != nil || !errors.Is(err, warden.ErrClosed) {
			t.Fatalf("isolated closed reader = %#v, %v", got, err)
		}
		failed, err := warden.Open(context.Background(), warden.Options{DatabasePath: path + ".missing"})
		requireFailedOpen(t, failed, err, warden.ErrDatabaseUnavailable, os.ErrNotExist)
		failed, err = warden.Open(context.Background(), warden.Options{})
		requireFailedOpen(t, failed, err, warden.ErrInvalidOptions, os.ErrInvalid)
		if calls := transport.calls.Load(); calls != 0 {
			t.Fatalf("library attempted %d HTTP requests", calls)
		}
		// Suppress the test runner's PASS line so the parent can check silence.
		os.Exit(0)
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=rw&_busy_timeout=0"}
	db, err := sql.Open("sqlite3", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	execute := func(query string) {
		t.Helper()
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	switch mode {
	case "exclusive":
		defer func() {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		}()
		execute("BEGIN EXCLUSIVE")
		if _, err := fmt.Fprintln(os.Stdout, "ready"); err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
			t.Fatal(err)
		}
		execute("ROLLBACK")
	case "hot-journal":
		execute("PRAGMA cache_size = 2")
		execute("BEGIN IMMEDIATE")
		execute("UPDATE mods SET version = 'uncommitted'")
		execute(`WITH RECURSIVE n(i) AS (VALUES(2) UNION ALL SELECT i+1 FROM n WHERE i < 100)
			INSERT INTO mods SELECT i, 'Plugin', 'Owner', '', 'uncommitted', NULL, printf('%04000d', i), 1 FROM n`)
		// Exit without closing SQLite to leave recovery to a writable owner.
		os.Exit(0)
	default:
		t.Fatalf("unknown fixture mode %q", mode)
	}
}

type rejectNetwork struct{ calls atomic.Int32 }

func (r *rejectNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls.Add(1)
	return nil, errors.New("unexpected network access")
}

func readerProcess(t *testing.T, path, mode string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReaderProcess$")
	command.Env = append(os.Environ(), "WARDEN_PUBLIC_READER_PROCESS="+mode, "WARDEN_PUBLIC_READER_DATABASE="+path)
	return command
}

func holdExclusiveWriter(t *testing.T, path string) func() {
	t.Helper()
	command := readerProcess(t, path, "exclusive")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		t.Helper()
		once.Do(func() {
			if err := input.Close(); err != nil {
				t.Error(err)
			}
			if err := command.Wait(); err != nil {
				t.Errorf("writer failed: %v\n%s", err, &stderr)
			}
		})
	}
	t.Cleanup(release)
	ready, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || ready != "ready\n" {
		t.Fatalf("writer did not acquire its lock: %q, %v", ready, err)
	}
	return release
}

func assertSQLiteError(t *testing.T, err, class error, code sqlite3.ErrNoExtended) {
	t.Helper()
	var cause sqlite3.Error
	if !errors.Is(err, class) || !errors.As(err, &cause) || cause.ExtendedCode != code {
		t.Fatalf("error = %v; want %v with SQLite code %v", err, class, code)
	}
}

type fileSnapshot struct {
	info     os.FileInfo
	contents []byte
}

func snapshotTree(t *testing.T, dir string) map[string]fileSnapshot {
	t.Helper()
	snapshot := make(map[string]fileSnapshot)
	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := fileSnapshot{info: info}
		if info.Mode().IsRegular() {
			item.contents, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		snapshot[path] = item
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertTree(t *testing.T, dir string, before map[string]fileSnapshot) {
	t.Helper()
	after := snapshotTree(t, dir)
	if len(after) != len(before) {
		t.Errorf("filesystem entry count changed: before=%d, after=%d", len(before), len(after))
	}
	for path, want := range before {
		got, ok := after[path]
		if !ok || !os.SameFile(want.info, got.info) || want.info.Mode() != got.info.Mode() ||
			!want.info.ModTime().Equal(got.info.ModTime()) || !bytes.Equal(want.contents, got.contents) {
			t.Errorf("reader changed filesystem state at %q", path)
		}
	}
}

func writeFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
}
