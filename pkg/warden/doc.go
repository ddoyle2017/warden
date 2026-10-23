// Package warden provides read-only access to Warden's recorded mod inventory
// and Thunderstore package metadata.
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
// # Catalog packages
//
// GetPackage accepts a PackageID with nonempty ASCII letters, digits, or
// underscores in each component. It fetches the package's current latest release,
// not the metadata for an installed version. The returned package and latest
// release must match the requested namespace, name, and full-name identities.
// A package page must have an absolute HTTP(S) URL. Release and dependency
// versions require unprefixed MAJOR.MINOR.PATCH SemVer, with optional prerelease
// and build metadata. Empty descriptions and website URLs, and a false Deprecated
// flag, are valid.
//
// A dependency list must be explicitly supplied: missing or null metadata is an
// error, while an explicit empty list returns a non-nil empty slice. Dependencies
// preserve declaration order, duplicate identities, framework tokens, and complete
// declared versions, including prerelease and build suffixes. They are metadata,
// not a resolved installation plan or evidence of compatibility. GetPackage does
// not fetch dependencies, skip framework metadata, or add catalog dependencies to
// InstalledMod records.
//
// Each call returns fresh caller-owned values. Failed lookups return a zero
// Package, never partial metadata. There is no library retry, cache, background
// refresh, or archive download. Calls after a successful Close return ErrClosed
// without making HTTP requests.
//
// Options.HTTPClient supplies a client reused unchanged across requests. The
// caller owns its configuration and shared transport; Reader.Close does not close
// idle HTTP connections. Configure the client before concurrent use. A nil client
// selects a reusable per-reader client with a 30-second timeout and the standard
// default transport. The caller's context reaches both the request and response
// body read. Open still performs no network access.
//
// # Errors
//
// Use errors.Is with ErrInvalidOptions, ErrDatabaseUnavailable,
// ErrDatabaseUnsupported, ErrDatabaseBusy, ErrDatabaseRecoveryRequired, or
// ErrClosed for reader lifecycle and storage failures. Catalog failures use
// ErrInvalidPackageID for invalid caller input, ErrPackageNotFound for HTTP 404,
// ErrInvalidMetadata for invalid JSON or incomplete/mismatched catalog values,
// and ErrRemoteFailure for transport, unexpected status, or body read/close
// failures. Catalog errors include the requested package identity.
//
// Classifications can overlap when failures are joined, such as invalid JSON
// followed by a body-close failure. Filesystem, SQLite, HTTP, JSON, body, and
// context causes remain wrapped for errors.Is and errors.As. Cancellation and
// deadlines match context.Canceled and context.DeadlineExceeded, including when
// a custom context cause is present, rather than returning successful empty data.
//
// The repository's docs/install-lifecycle.md describes storage compatibility,
// installation-record semantics, and recovery limitations. CONTRIBUTING.md
// documents CGO setup and isolated CLI execution.
package warden
