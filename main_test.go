package main_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
)

func TestCLIExitStatus(t *testing.T) {
	binary := buildCLI(t)
	for name, tt := range map[string]struct {
		args      []string
		input     string
		prepare   func(*testing.T, cliFixture)
		verify    func(*testing.T, cliFixture)
		exit      int
		usage     bool
		stdout    []string
		stderr    []string
		forbidden []string
	}{
		"given an empty installation, when listing, then exit successfully": {
			args: []string{"list"}, stdout: []string{"... no mods are installed..."},
		},
		"given a fixture script, when starting, then preserve successful output and exit zero": {
			args: []string{"start", "modded"}, prepare: prepareHealthyStart,
			stdout: []string{"... successfully started server! ...", "Starting Modded Server"},
		},
		"given a missing mod, when updating, then exit one with one runtime diagnostic": {
			args: []string{"update", "--mod", "NotInstalled"}, exit: 1,
			stderr: []string{"mod not installed, update stopped"},
		},
		"given a missing qualified mod, when updating, then exit one without falling back": {
			args: []string{"update", "--namespace", "", "--mod", "NotInstalled"}, exit: 1,
			stderr: []string{"mod not installed, update stopped"},
		},
		"given a missing required flag, when updating, then reject input with usage once": {
			args: []string{"update"}, exit: 1, usage: true,
			stderr: []string{`required flag(s) "mod" not set`},
		},
		"given an invalid game type, when starting, then diagnose the type and show usage once": {
			args: []string{"start", "invalid"}, exit: 1, usage: true,
			stderr: []string{"invalid game type"}, forbidden: []string{"Valheim server failed to start"},
		},
		"given a missing startup script, when starting a valid type, then report a start failure": {
			args: []string{"start", "modded"}, exit: 1,
			stderr: []string{"Valheim server failed to start"}, forbidden: []string{"invalid game type"},
		},
		"given a failing owned script, when starting, then preserve process output and exit one": {
			args: []string{"start", "modded"}, exit: 1,
			prepare: func(t *testing.T, f cliFixture) {
				writeCLIFile(t, filepath.Join(f.server, "start_server_bepinex.sh"), "printf 'owned process failure\\n'\nexit 7\n")
			},
			stdout: []string{"owned process failure"}, stderr: []string{"Valheim server failed to start"},
			forbidden: []string{"invalid game type"},
		},
		"given an unknown configuration key, when getting it, then exit one": {
			args: []string{"config", "get", "missing-key"}, exit: 1,
			stderr: []string{"configuration key does not exist"},
		},
		"given an invalid configuration key, when setting it, then reject it without saving": {
			args: []string{"config", "set", "invalid-key", "invalid-value"}, exit: 1,
			stderr: []string{"'invalid-key' is not a valid config setting"},
			verify: func(t *testing.T, f cliFixture) {
				data, err := os.ReadFile(filepath.Join(f.home, ".warden.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "invalid-key") || !strings.Contains(string(data), f.server) {
					t.Errorf("rejected configuration changed persisted settings: %s", data)
				}
			},
		},
		"given EOF, when confirming removal, then exit one and retain the input cause": {
			args: []string{"remove", "--namespace", "Owner", "--mod", "NotInstalled"}, exit: 1,
			stderr: []string{"unable to read confirmation", "EOF"},
		},
		"given two invalid answers, when confirming removal, then exit one without success": {
			args:  []string{"remove", "--namespace", "Owner", "--mod", "NotInstalled"},
			input: "invalid\ninvalid\nY\n", exit: 1, stderr: []string{"reached max confirmation attempts"},
		},
		"given deliberate refusal, when removing, then exit zero with only the abort outcome": {
			args:  []string{"remove", "--namespace", "Owner", "--mod", "NotInstalled"},
			input: "n\n", stdout: []string{"... aborting ..."}, forbidden: []string{"successfully"},
		},
		"given deliberate bootstrap refusal, when adding, then exit zero without fetching the mod": {
			args:  []string{"add", "--namespace", "Owner", "--mod", "NotInstalled"},
			input: "n\n", stdout: []string{"... aborting ..."}, forbidden: []string{"successfully"},
		},
		"given no installed mods, when updating all, then exit zero without claiming a mutation": {
			args: []string{"update", "all"}, input: "Y\n",
			stdout: []string{"... no mods need updating ..."}, forbidden: []string{"successfully"},
		},
		"given unreadable mod records, when listing, then exit one without an empty-list fallback": {
			args: []string{"list"}, exit: 1,
			prepare: func(t *testing.T, f cliFixture) {
				db := openCLIDatabase(t, f)
				if _, err := db.Exec(`DROP TABLE mods; CREATE TABLE mods (id INTEGER); INSERT INTO mods VALUES (1)`); err != nil {
					t.Fatal(err)
				}
			},
			stderr:    []string{"unable to retrieve list of mods", repo.ErrModMappingFailed.Error()},
			forbidden: []string{"no mods are installed"},
		},
		"given rejected removal metadata, when removing, then exit one and preserve recovery guidance and state": {
			args: []string{"remove", "--namespace", "Owner", "--mod", "Installed"}, input: "Y\n", exit: 1,
			prepare: prepareRejectedRemoval,
			stderr:  []string{"verify database state", "owned metadata failure"},
			verify: func(t *testing.T, f cliFixture) {
				db := openCLIDatabase(t, f)
				record, err := repo.NewModsRepo(db).GetModByIdentity("Owner", "Installed")
				if err != nil {
					t.Fatal(err)
				}
				wantPath := filepath.Join(f.server, "BepInEx", "plugins", "Owner-Installed-1.0.0")
				if record.Version != "1.0.0" || record.FilePath != wantPath {
					t.Errorf("failed deletion changed the record: %+v", record)
				}
				data, err := os.ReadFile(filepath.Join(wantPath, "plugin.dll"))
				if err != nil || string(data) != "original plugin" {
					t.Errorf("failed deletion did not restore original files: %q, %v", data, err)
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newCLIFixture(t, binary)
			if tt.prepare != nil {
				tt.prepare(t, f)
			}
			stdout, stderr, exit := f.run(t, tt.input, tt.args...)
			if exit != tt.exit {
				t.Errorf("exit = %d, want %d; stdout=%q, stderr=%q", exit, tt.exit, stdout, stderr)
			}
			for _, want := range tt.stdout {
				if strings.Count(stdout, want) != 1 {
					t.Errorf("stdout must contain %q once: %q", want, stdout)
				}
			}
			for _, want := range tt.stderr {
				if strings.Count(stderr, want) != 1 {
					t.Errorf("stderr must contain %q once: %q", want, stderr)
				}
			}
			wantErrors, wantUsage := 0, 0
			if tt.exit != 0 {
				wantErrors = 1
				if strings.Contains(stdout+stderr, "successfully") {
					t.Errorf("failure reported success: %q, %q", stdout, stderr)
				}
			} else if stderr != "" {
				t.Errorf("successful command wrote an error: %q", stderr)
			}
			if tt.usage {
				wantUsage = 1
			}
			if strings.Count(stderr, "Error:") != wantErrors || strings.Count(stdout+stderr, "Usage:") != wantUsage {
				t.Errorf("unexpected error/usage reporting: stdout=%q, stderr=%q", stdout, stderr)
			}
			for _, forbidden := range tt.forbidden {
				if strings.Contains(stdout+stderr, forbidden) {
					t.Errorf("output contains misleading %q: stdout=%q, stderr=%q", forbidden, stdout, stderr)
				}
			}
			if tt.verify != nil {
				tt.verify(t, f)
			}
		})
	}
}

func buildCLI(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "warden")
	args := []string{"build", "-p", "2", "-o", binary}
	if runtime.GOOS == "darwin" {
		args = append(args, "-ldflags=-linkmode=external")
	}
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), append(args, ".")...)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	if runtime.GOOS == "darwin" {
		// Older Go linkers can leave an invalid Mach-O signature, even with external linking.
		sign := exec.Command("codesign", "--force", "--sign", "-", binary)
		if output, err := sign.CombinedOutput(); err != nil {
			t.Fatalf("sign owned CLI fixture: %v\n%s", err, output)
		}
	}
	return binary
}

type cliFixture struct {
	binary, home, server, scratch, proxy string
}

func newCLIFixture(t *testing.T, binary string) cliFixture {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected network request from offline CLI fixture: %s %s", r.Method, r.URL)
		http.Error(w, "offline fixture", http.StatusBadGateway)
	}))
	t.Cleanup(proxy.Close)
	f := cliFixture{binary: binary, home: t.TempDir(), server: t.TempDir(), scratch: t.TempDir(), proxy: proxy.URL}
	if stdout, stderr, exit := f.run(t, "", "config", "set", "valheim-directory", f.server); exit != 0 {
		t.Fatalf("healthy configuration control failed: exit=%d, stdout=%q, stderr=%q", exit, stdout, stderr)
	}
	if stdout, stderr, exit := f.run(t, "", "--help"); exit != 0 || !strings.Contains(stdout, "Usage:") || stderr != "" {
		t.Fatalf("healthy binary launch control failed: exit=%d, stdout=%q, stderr=%q", exit, stdout, stderr)
	}
	if stdout, stderr, exit := f.run(t, "", "config", "get", "valheim-directory"); exit != 0 || strings.TrimSpace(stdout) != f.server || stderr != "" {
		t.Fatalf("isolated server configuration was not saved: exit=%d, stdout=%q, stderr=%q", exit, stdout, stderr)
	}
	return f
}

func (f cliFixture) run(t *testing.T, input string, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.binary, args...)
	cmd.Env = append(os.Environ(), "HOME="+f.home, "TMPDIR="+f.scratch,
		"HTTP_PROXY="+f.proxy, "HTTPS_PROXY="+f.proxy, "NO_PROXY=", "no_proxy=")
	cmd.Dir = f.scratch
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI fixture timed out: %v", ctx.Err())
	}
	exit := 0
	if err != nil {
		var failure *exec.ExitError
		if !errors.As(err, &failure) {
			t.Fatalf("CLI could not launch: %v", err)
		}
		exit = failure.ExitCode()
	}
	return stdout.String(), stderr.String(), exit
}

func prepareHealthyStart(t *testing.T, f cliFixture) {
	t.Helper()
	data, err := os.ReadFile("internal/test/data/start_server_bepinex.sh")
	if err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(f.server, "start_server_bepinex.sh"), string(data))
}

func prepareRejectedRemoval(t *testing.T, f cliFixture) {
	t.Helper()
	db := openCLIDatabase(t, f)
	path := filepath.Join(f.server, "BepInEx", "plugins", "Owner-Installed-1.0.0")
	if err := repo.NewModsRepo(db).InsertMod(mod.Mod{
		Namespace: "Owner", Name: "Installed", Version: "1.0.0", FilePath: path,
	}); err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(path, "plugin.dll"), "original plugin")
	if _, err := db.Exec(`CREATE TRIGGER reject_removal BEFORE DELETE ON mods
		BEGIN SELECT RAISE(FAIL, 'owned metadata failure'); END`); err != nil {
		t.Fatal(err)
	}
}

func openCLIDatabase(t *testing.T, f cliFixture) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(f.home, ".warden.db"))
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

func writeCLIFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
