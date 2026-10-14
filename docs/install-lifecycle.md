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
| Remove a mod | Confirm, take a complete backup and stage its versioned directory outside the plugin tree, delete its record, then discard staged files and the backup. |
| Remove all mods | Require the stronger confirmation, back up and stage the plugin directory, recreate an empty plugin directory, delete the records, then discard staged files and the backup. |
| Install BepInEx | Download and unpack the framework into the server directory, move the archive wrapper's contents into place, then insert the framework record. |
| Update BepInEx | Move plugins to temporary storage, remove and install framework files, move plugins back, then update the framework record. |
| Remove BepInEx | Remove the framework's known files, including the plugin directory, then delete its framework record. The current path does not also delete mod records. |

Installing a root mod can succeed before a dependency fails. Updating a mod
removes its old files before the new installation succeeds. These are not
all-or-nothing operations.

Initial and dependency installations write records only after file installation
succeeds. A failed file-install step does not delete pre-existing records, so a
dependency backup failure preserves its earlier record and files. Explicit
updates have already removed the old release: if replacement files fail, the
update attempts to delete that obsolete record. Failure to write replacement
metadata after successful file installation does not trigger that cleanup.

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

Mod removal retains original files in staging beside the plugin directory until
record deletion succeeds. Backup or staging failure stops before record deletion.
Staging uses a rename; a cross-filesystem rename fails rather than falling back
to destructive copying. If record deletion returns an error, the service attempts
to rename the staged files back. A conflicting destination is not overwritten;
the error identifies the retained staging path for recovery. A repository error
does not prove that its write was unapplied, so inspect recorded state as well.

After successful record deletion, removal is committed and cleanup is
irreversible. Cleanup failures identify leftover paths but do not make those
potentially partial contents usable recovery snapshots. File-only removal callers
can distinguish this committed outcome through `ModRemovalCleanupError`; mod
updates report it and continue installing the replacement rather than stopping
after the old files have already been removed.

Single/all mod removal commands display the wrapped recovery details, including
retained paths and instructions to verify database state. A committed cleanup
error is shown as a completed removal with a cleanup warning, not as a failed
removal.

Restoration copies the snapshot into staging beside the destination before
renaming any live files. The snapshot itself is never renamed across
filesystems. A preparation failure leaves live files and the active backup
unchanged. Publishing first moves the prior tree aside; if publishing fails,
restoration moves that tree back. If recovery also fails, the error identifies
the retained prior tree, prepared replacement, and original backup. Keep these
paths until the intended tree has been recovered and inspected.

Successful publication consumes the active backup, even if later cleanup
fails. Cleanup errors identify leftover paths; a partially deleted snapshot is
not offered for another restore. The restored destination remains available.
Missing destinations and parents are supported, but restoration rejects
non-directory or symlink targets, filesystem roots, working-directory
ancestors, and paths overlapping the backup, including resolved aliases.
Restoration needs space and write access in the destination's parent and a
destination that can be renamed. A mount point that cannot be renamed fails
without deleting its live contents. The rename/recovery sequence is not
crash-atomic and does not guard against concurrent filesystem changes.

Framework updates still move plugins to system-temporary storage using
rename-only transfers, so plugin staging can fail across filesystems. Updates
also reuse one server backup object across nested operations, so a later
successful backup can replace the snapshot needed by the outer update;
successful nested cleanup can leave no active server backup. Some existing
callers still discard restoration, cleanup, or underlying file-layer error
details. Framework updates also replace existing BepInEx configuration rather
than preserving it. Safe backup restoration does not establish safe plugin
staging, whole-update rollback, database recovery, or preservation of user-owned
framework configuration.

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
