# Contributing to Warden

Warden changes can affect a game server's files as well as local metadata.
Develop against temporary state, and check the relevant behavior before trying
a change on an existing installation.

## Prerequisites

- Install Go using the version declared in [go.mod](go.mod) when reproducing CI.
  The workflow reads that file rather than maintaining a separate version.
- Enable CGO and provide a C compiler. The
  [SQLite driver](https://github.com/mattn/go-sqlite3/tree/v1.14.22#installation)
  uses CGO; a `CGO_ENABLED=0` build is not a working SQLite-backed Warden.
  GCC on Linux or the Xcode Command Line Tools compiler on macOS is sufficient.
- Use a POSIX shell for the commands below and for server-start fixture tests.
  Linux is the CI target. Platform constants and script names in the code are
  not a guarantee that every operation works on Windows or macOS.

Run commands from the repository root. Go downloads module dependencies as
needed; after dependencies are available, tests use local fixtures and mocked
HTTP responses, not Thunderstore or a Valheim server.

## Build and check

These are the checks used by [CI](.github/workflows/build.yml):

```sh
export CGO_ENABLED=1

unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
  printf 'Run gofmt on:\n%s\n' "$unformatted"
  exit 1
fi

go build ./...
go vet ./...
go test -race -shuffle=on -count=1 ./...
```

Format only the Go files you changed with `gofmt -w path/to/file.go`. During
development, start with the relevant packages rather than repeatedly running
the whole suite:

```sh
go test ./internal/api/thunderstore ./internal/service
go test ./internal/data/file ./internal/data/repo
```

For state-leak or order-sensitive failures, repeat the affected packages with
`-count=10 -shuffle=on`. Go prints the shuffle seed; use `-shuffle=<seed>` to
reproduce the top-level test order. Map-based subtest order is independently
unspecified, so each case must own its state.

On macOS, an older Go toolchain can fail before tests start with
`missing LC_UUID load command`. If that occurs, use the external linker for
the local test run:

```sh
go test -race -shuffle=on -count=1 -ldflags=-linkmode=external ./...
```

The same linker flag can be passed to `go build` if needed. This is a local
toolchain workaround; Linux CI uses the commands above without it.

CLI subprocess tests build an owned temporary executable, use external linking
and ad-hoc signing on macOS, and require a healthy launch before checking failure
exits. A loader or signature rejection is not evidence that a CLI failure
returned the correct status.

## Tests and fixtures

`internal/test/data` contains read-only mod/framework ZIPs and small shell
scripts that print fixture output. Server-start tests execute those scripts,
not the game. Do not replace them with live server scripts or refresh archives
from the network as part of an ordinary test run.

`internal/test/helper.NewHelper(t)` creates temporary server and database
directories and registers database cleanup. Create it inside each stateful
subtest so cases cannot affect each other's records or files. Database
connections close before their files are removed. Other writable fixtures
belong in `t.TempDir()`; no manually created `internal/test/config` or
`internal/test/file` directory is required.

Configuration still uses Viper's package-global state. Reset it before a case
and register a reset with `t.Cleanup`; temporary directories do not make global
Viper state safe for parallel tests.

Use the existing function-field mocks to inject external failures and observe
significant interactions. For SQL preparation failures, a real temporary
database without the required table exercises the failure without incomplete
transaction mocks. Full testing conventions live in
[tests.instructions.md](.github/instructions/tests.instructions.md).

Thunderstore metadata fixtures configure `mock.HTTPClient.DoFunc` so they can
inspect the request context, method, and escaped endpoint. Archive-download
fixtures still use `GetFunc`. Configure each callback the scenario reaches
explicitly, and create a fresh response body inside it for every invocation;
reusing a consumed or closed body can make later calls exercise the wrong path.

## Isolated CLI smoke check

Even `warden --help` loads or creates `$HOME/.warden.yaml` and initializes
`$HOME/.warden.db` before Cobra dispatches the command. Build first, then give
the executable an isolated home and server directory:

```sh
scratch=$(mktemp -d)
go build -o "$scratch/warden" .
mkdir "$scratch/home" "$scratch/server"

HOME="$scratch/home" "$scratch/warden" config set valheim-directory "$scratch/server"
HOME="$scratch/home" "$scratch/warden" --help
HOME="$scratch/home" "$scratch/warden" config
HOME="$scratch/home" "$scratch/warden" list

printf 'Inspect and then remove this scratch directory: %s\n' "$scratch"
```

Use the temporary home on **every** invocation, not just the first one. Do not
run `add`, `update`, `remove`, or `start` against a real installation merely to
validate a code change. The commands above do not download mods or start a
server. Remove only the scratch directory printed by this procedure when
finished.

## Changes that cross boundaries

Commands own the CLI contract, services coordinate work, and repositories and
file managers own their respective state. Keep new behavior in the appropriate
layer and wire dependencies explicitly. The
[Go conventions](.github/instructions/go.instructions.md) describe coding
preferences without requiring unrelated legacy code to be rewritten.

Before changing mod or framework operations, read the
[installation lifecycle](docs/install-lifecycle.md). Keep that reference
current when ordering or failure behavior changes. Keep user-facing syntax and
configuration in the [README](README.md); do not duplicate either guide inside
every instruction file or skill.
