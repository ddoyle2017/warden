package repo_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
)

type sqliteWriterReply struct {
	Error string
	Code  int
}

// The writer is a separate process so raw-file assertions and header inspection
// cannot inadvertently release its POSIX SQLite locks.
func TestReadOnlyWriterProcess(t *testing.T) {
	path := os.Getenv("WARDEN_TEST_SQLITE_WRITER")
	if path == "" {
		return
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw&_busy_timeout=0"}
	db, err := sql.Open("sqlite3", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	encoder, decoder := json.NewEncoder(os.Stdout), json.NewDecoder(os.Stdin)
	if err := encoder.Encode(sqliteWriterReply{}); err != nil {
		t.Fatal(err)
	}
	for {
		var query string
		if err := decoder.Decode(&query); err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			return
		}
		reply := sqliteWriterReply{}
		if _, err := db.Exec(query); err != nil {
			reply.Error = err.Error()
			var cause sqlite3.Error
			if errors.As(err, &cause) {
				reply.Code = int(cause.Code)
			}
		}
		if err := encoder.Encode(reply); err != nil {
			t.Fatal(err)
		}
	}
}

type sqliteWriter struct {
	t       *testing.T
	command *exec.Cmd
	input   io.WriteCloser
	encoder *json.Encoder
	decoder *json.Decoder
	stderr  *bytes.Buffer
	stopped sync.Once
}

func startSQLiteWriter(t *testing.T, path string) *sqliteWriter {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReadOnlyWriterProcess$")
	command.Env = append(os.Environ(), "WARDEN_TEST_SQLITE_WRITER="+path)
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
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
	writer := &sqliteWriter{
		t: t, command: command, input: input, encoder: json.NewEncoder(input),
		decoder: json.NewDecoder(output), stderr: stderr,
	}
	t.Cleanup(writer.stop)
	var ready sqliteWriterReply
	if err := writer.decoder.Decode(&ready); err != nil || ready.Error != "" {
		t.Fatalf("writer did not start: %+v, %v", ready, err)
	}
	return writer
}

func (w *sqliteWriter) execute(query string) sqliteWriterReply {
	w.t.Helper()
	if err := w.encoder.Encode(query); err != nil {
		w.t.Fatal(err)
	}
	var reply sqliteWriterReply
	if err := w.decoder.Decode(&reply); err != nil {
		w.t.Fatalf("writer reply for %q: %v", query, err)
	}
	return reply
}

func (w *sqliteWriter) mustExec(query string) {
	w.t.Helper()
	if reply := w.execute(query); reply.Error != "" {
		w.t.Fatalf("writer query %q: %+v", query, reply)
	}
}

func (w *sqliteWriter) stop() {
	w.t.Helper()
	w.stopped.Do(func() {
		if err := w.input.Close(); err != nil {
			w.t.Error(err)
		}
		if err := w.command.Wait(); err != nil {
			w.t.Errorf("writer failed: %v\n%s", err, w.stderr)
		}
	})
}

func (w *sqliteWriter) crash() {
	w.t.Helper()
	w.stopped.Do(func() {
		if err := w.command.Process.Kill(); err != nil {
			w.t.Fatal(err)
		}
		if err := w.input.Close(); err != nil {
			w.t.Error(err)
		}
		var exit *exec.ExitError
		if err := w.command.Wait(); !errors.As(err, &exit) {
			w.t.Fatalf("writer did not crash: %v", err)
		}
	})
}
