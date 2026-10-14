# Installation lifecycle

Warden coordinates downloaded files with SQLite metadata. Those are separate
resources: a database transaction cannot undo a filesystem operation, and a
filesystem backup does not restore database records. Changes to either side
must account for what remains when the other side fails.

This reference describes current behavior and its limitations, not a guarantee
of atomic installation or automatic recovery.

## Package, release, and framework identity

A Thunderstore package has a namespace (its author or owner) and a name. A
release adds a version; its full name is `namespace-name-version`. Installed
mod directories use this versioned full name, while CLI flags accept package
name and, for add/remove, namespace separately.

Repository reads and upserts currently identify mods by name; single-mod
deletion also includes namespace. Do not assume every operation distinguishes
two authors publishing a package with the same name.

BepInEx is a framework, not an ordinary plugin. Its archive contains a wrapper
directory whose contents are moved into the Valheim server directory. Ordinary
mods are extracted into individual directories under `BepInEx/plugins`.
Framework updates therefore need to preserve plugins while replacing framework
files.

Dependency strings include namespace, name, and version. Current dependency
handling fetches the latest release for each direct dependency, rather than
honoring the version in that string or recursively traversing the dependency
graph. BepInEx is skipped by the mod dependency loop and handled separately.
Version comparisons currently use string ordering, not semantic-version
ordering. Treat changes to these behaviors as explicit product decisions.

## Operation ordering

The following describes the affirmative path after any required confirmation.
Earlier completed steps can remain in effect if a later step fails.

| Operation | Ordering and scope |
| --- | --- |
| Add a mod | The command first attempts BepInEx installation when its record is missing. If that returns no error, the mod service checks for an existing mod, fetches the latest release, installs files, upserts the record, and then installs direct dependencies. |
| Update a mod | Fetch the installed record and latest release. If the version compares newer and the user confirms, remove the old release directory, install the new files, upsert the record, and process dependencies. |
| Update all mods | Confirm once, then process recorded mods sequentially. A failure stops the loop; earlier successful updates are not undone. This is not a framework update. |
| Remove a mod | Confirm, delete its record, then remove its versioned directory. A file-removal failure can leave unrecorded files. |
| Remove all mods | Require the stronger confirmation, then attempt both record deletion and plugin-directory removal, even if record deletion fails. The file manager recreates an empty plugin directory. |
| Install BepInEx | Download and unpack the framework into the server directory, move the archive wrapper's contents into place, then insert the framework record. |
| Update BepInEx | Move plugins to temporary storage, remove and install framework files, move plugins back, then update the framework record. |
| Remove BepInEx | Remove the framework's known files, including the plugin directory, then delete its framework record. The current path does not also delete mod records. |

Installing a root mod can succeed before a dependency fails. Updating a mod
removes its old files before the new installation succeeds. These are not
all-or-nothing operations.

## Confirmations and backups

Confirmation inputs are case-sensitive. Ordinary prompts accept `Y` or `n`;
removing all mods or BepInEx uses `YES I AM` or `no`. Invalid input is bounded.
A negative answer returns nil, and exhausted input can also return nil without
a mutation. Callers must not interpret every nil error as proof that an
installation, update, or removal occurred.

The file manager makes a backup available for restoration only after the
complete copy succeeds. After a failed copy, it attempts to remove the temporary
directory and returns any cleanup error alongside the creation error. A failed
attempt leaves any previous valid snapshot active. Mod and framework operations
stop if a required backup cannot be created rather than continuing with an
incomplete snapshot.

An empty source directory is valid for a backup; a missing source is an error.
Mod installation and removal of all mods create a missing plugins directory
before taking a backup. Removing an absent mod remains a no-op.

Restoration still removes the destination before moving backed-up files, and
those moves do not support cross-filesystem transfer. Framework updates still
reuse one backup object across nested operations, so a later successful backup
can replace the snapshot needed by the outer update. Some restoration and
post-operation cleanup errors are still ignored. Framework updates also replace
existing BepInEx configuration rather than preserving it. These limitations
mean temporary backups do not establish successful rollback or preservation of
the caller's original state.

When changing these paths, define the observable filesystem and database state
for success, cancellation, and each failure being handled. Verify those states,
including existing plugin contents and unrelated server files, rather than
checking only the returned error.

## Before changing a live installation

Stop the game server and avoid concurrent Warden operations. Preserve an
independent snapshot of the server directory, `.warden.db`, and `.warden.yaml`
before an operation whose outcome you cannot safely discard. Warden's temporary
backups are not a substitute for that snapshot.

After a partial failure, inspect both installed files and recorded metadata
before retrying. `warden list` reads metadata; it does not prove the matching
files are present. There is no repair or reconciliation command. If restoring
a known-good snapshot, restore the corresponding server files and metadata
together rather than deleting the database to silence an error.

For development and regression scenarios, use the temporary fixtures and
isolated execution procedure in [Contributing](../CONTRIBUTING.md), never a
live server.
