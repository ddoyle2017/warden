// Package warden provides read-only access to Warden's recorded mod inventory.
//
// # Startup and ownership
//
// Call Open with an explicit Options.DatabasePath naming an existing SQLite
// database. Relative paths are resolved against the working directory; paths are
// not expanded from environment variables or discovered from the user's home or
// configuration. SQLite connection strings and in-memory databases are rejected.
// Open, ListInstalledMods, and Close do not initialize or migrate storage, read
// configuration or server files, contact Thunderstore, download archives, prompt,
// or print to stdout or stderr. The SQLite driver requires CGO and a C compiler.
//
// Share one *Reader per database per process among concurrent callers. A second
// Open for the same file, including symlink and hard-link aliases, returns
// ErrDatabaseBusy. The lifecycle owner must stop and drain requests, then handle
// the error from Close. Ownership is released only after SQL resources close
// successfully; a close failure is retained, not treated as permission to reopen.
// Do not copy a Reader or open uncoordinated writable SQLite handles to its
// database in the same process.
//
// # Storage and inventory
//
// Only compatible rollback-journal databases are supported. WAL databases are
// rejected before SQLite opens them, even when their sidecars already exist.
// Databases needing journal recovery are rejected without recovery or file
// mutation. Repair belongs to a writable owner, not this library.
//
// Ordinary writes from a separate Warden CLI process are supported: later reads
// see later committed changes, and contention returns ErrDatabaseBusy without
// hidden retries. This is not an immutable snapshot. Readers and writers must use
// a consistent pathname so SQLite finds the matching journal. Stop and close the
// Reader before changing journal mode or schema, replacing the database file, or
// changing filesystem aliases. Linux is the primary target; this API does not
// establish general macOS or Windows support.
//
// ListInstalledMods returns only mod rows, not framework rows. Its values are
// caller-owned and preserve recorded namespace, name, version, path, and duplicate
// identities without checking files or release compatibility. No dependencies
// are inferred. Ordering is case-sensitive lexicographic namespace, name, version,
// then installation path, not semantic-version ordering. An empty compatible
// inventory is a non-nil empty slice. Any failed read returns nil, never partial
// results or a successful empty fallback.
//
// # Errors
//
// Use errors.Is with ErrInvalidOptions, ErrDatabaseUnavailable,
// ErrDatabaseUnsupported, ErrDatabaseBusy, ErrDatabaseRecoveryRequired, or
// ErrClosed. Filesystem, SQLite, and context causes remain wrapped for errors.Is
// and errors.As; cancellation and deadlines match context.Canceled and
// context.DeadlineExceeded rather than being reported as an empty inventory.
//
// The repository's docs/install-lifecycle.md describes storage compatibility,
// installation-record semantics, and recovery limitations. CONTRIBUTING.md
// documents CGO setup and isolated CLI execution.
package warden
