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
mod directories use this versioned full name, while CLI flags accept namespace
and package name separately.

Lookup, installation/upsert, and single-mod removal identify a package by both
namespace and name. Two authors' same-name packages have separate records and
release directories; updating one retains its record ID. An explicitly
qualified lookup never falls back to another namespace.

Name-only updates remain supported when exactly one installed record matches.
An ambiguous name stops the update before a package fetch or file changes and
lists the matching namespace/name pairs. Use the namespace-qualified syntax in
the [README](../README.md#usage) to select a package. If listing the matches
fails, the error is reported without proceeding. Query, mapping, and row-iteration
errors are not evidence that a mod is absent: upsert returns the error rather
than inserting a new record, retaining its database cause.

The existing database schema already stores namespace and name separately, so
this identity rule requires no schema migration or recreation. Duplicate records
for the same namespace and name are not automatically reconciled; exact lookups
and upserts reject them rather than selecting or overwriting one. Updates
identify duplicated packages and direct the user to inspect installation records
before retrying, rather than suggesting that namespace qualification can repair
duplicate rows.

BepInEx is a framework, not an ordinary plugin. Its archive contains a wrapper
directory whose contents are moved into the Valheim server directory. Ordinary
mods are extracted into individual directories under `BepInEx/plugins`.
Framework updates preserve plugins and user configuration while replacing
framework files.

## Release version ordering

Single-mod, all-mod, and BepInEx updates compare the recorded version with
Thunderstore's latest release using [Semantic Versioning 2.0.0](https://semver.org/).
Warden requires the full, unprefixed `MAJOR.MINOR.PATCH` form documented for
[Thunderstore packages](https://wiki.thunderstore.io/mods/creating-a-package#version-number).
Components compare numerically, so `1.10.0` is newer than `1.9.0`. An equal or
older available release does not trigger an update.

SemVer prerelease and build suffixes are supported: `1.0.0-beta.10` is newer
than `1.0.0-beta.9` but older than `1.0.0`. Build metadata does not affect
precedence, so `1.0.0+build.1` and `1.0.0+build.2` compare equal. The complete
original version and release name are retained when installing and recording
an update.

Empty or malformed versions, a `v` prefix, abbreviated or four-component
versions, and leading zeroes in core or numeric prerelease components are
rejected. An invalid installed or latest version returns an error identifying
the package and offending value before changing that package's files or record.
Bulk updates still run sequentially: an invalid version stops the loop without
undoing earlier successful updates. This comparison policy does not change
dependency selection or add a prerelease channel filter.

## Direct dependency validation

Dependency tokens use Thunderstore's
[`namespace-name-version` format](https://wiki.thunderstore.io/mods/creating-a-package#dependencies).
Namespace and name must be nonempty and use ASCII letters, digits, or underscores,
following Thunderstore's
[component alphabet](https://github.com/thunderstore-io/Thunderstore/blob/685b2743035e0ac0786180d10813d67b72fcdc2d/django/thunderstore/repository/consts.py).
The complete version suffix follows the [release version policy](#release-version-ordering),
including prerelease hyphens and build metadata.

An add validates the entire direct-dependency list after fetching root release
metadata, before downloading its archive, staging files, writing its record, or
fetching dependencies. New roots use the latest release; existing roots use
their recorded version as described in [retrying an add](#retrying-an-add).
Updates validate only when a newer release is selected
and confirmation succeeds. No-update, declined, and confirmation-input outcomes
stop before dependency validation. Bulk updates validate each selected package
immediately before its replacement, including replacement as an earlier root's
dependency, not every package before the loop.

Invalid input returns `ErrAddDependenciesFailed` and `ErrInvalidDependency`,
identifying the root package and offending token. Invalid dependency versions
also retain `ErrInvalidReleaseVersion` for `errors.Is`. No part of that root's
plan is installed or recorded, even when a malformed token follows valid ones.

For each dependency it installs, Warden selects the latest release,
not the version declared in its token, and does not traverse dependencies
recursively. BepInEx tokens are validated too, but skipped during mod dependency
installation; framework bootstrap remains a separate command step.

## Retrying an add

Repeat the same namespace-qualified `add` after a direct-dependency failure to
finish missing dependency installations. Warden retains an existing root's files
and complete record instead of downloading, reinstalling, or upgrading it. This
also works for partial installations left by older Warden versions.

SQLite stores the root's version but not its dependency list. Warden retrieves
the installed namespace, name, and version through Thunderstore's
[exact-version endpoint](https://github.com/thunderstore-io/Thunderstore/blob/685b2743035e0ac0786180d10813d67b72fcdc2d/django/thunderstore/repository/api/experimental/urls.py).
The response must identify that exact release and explicitly include its
dependency list. A changed latest root's list is never substituted while keeping
older root files. Unavailable, mismatched, incomplete, or malformed metadata
stops the retry without changing the root or its dependencies; retry when valid
metadata for the installed version is available. Even a no-work repeat requires
this metadata lookup.

After validating the complete plan, an add looks up every non-framework direct
dependency by namespace and name before installing any dependency. Only a genuine
no-results lookup makes it eligible for installation. Query, mapping, iteration,
and ambiguous-result errors stop dependency work, though a newly added root may
already be installed and recorded. Duplicate dependency identities are installed
only once.

An add reuses every recorded dependency without fetching its latest release or
changing its files or record. It does not enforce the version declared in the
token, downgrade a newer recorded dependency, or implicitly upgrade an older
one. Use `update` deliberately to change recorded packages; update behavior is
unchanged by add retries. If no direct dependencies are missing, an existing
root returns the usual already-installed result without mutation.

Here, "missing" means no installation record, not a failed file-integrity or
compatibility check. This is not a repair of missing root files, damaged recorded
dependencies, or untracked files, and it does not adopt orphaned installations.
No database migration or recursive dependency resolver is involved. A later
failure retains earlier completed work and cleanup diagnostics. After a metadata
write failure, verify database and file state before retrying; a failed write may
have committed.

## Operation ordering

The following describes the affirmative path after any required confirmation.
Earlier completed steps can remain in effect if a later step fails.

| Operation | Ordering and scope |
| --- | --- |
| Add a mod | Establish framework readiness from an existing record or completed bootstrap. Only then fetch the latest release for a new root or exact-version metadata for an existing root, validate the direct-dependency plan, install and record only a new root, and install missing direct dependencies. |
| Update a mod | Fetch the installed record and latest release. If the version compares newer and the user confirms, validate the direct-dependency plan and target, prepare the replacement, retain the original outside plugins, publish the replacement, upsert its record, finalize original-file cleanup, and process dependencies. |
| Update all mods | Confirm once, then process recorded mods and their direct dependencies. Before fetching each listed mod or selected dependency, reject duplicate namespace/name identities from the original list. Installed dependencies in the selection are validated and replaced immediately when needed. A failure stops the loop; earlier successful updates are not undone. This is not a framework update. |
| Remove a mod | Confirm, look up the exact namespace and name, take a complete backup and stage that record's versioned directory outside the plugin tree, delete the matching record, then discard staged files and the backup. |
| Remove all mods | Require the stronger confirmation, back up and stage the plugin directory, recreate an empty plugin directory, delete the records, then discard staged files and the backup. |
| Install BepInEx | Download and unpack the framework into the server directory, move the archive wrapper's contents into place, then insert the framework record. |
| Update BepInEx | Take one original-server snapshot, copy plugins to a temporary sibling of the server directory, remove and install framework files, preserve existing configuration over packaged defaults, transfer plugin copies through destination-local staging, finalize the server snapshot, then update the framework record. |
| Remove BepInEx | Remove the framework's known files, including the plugin directory, then delete matching plugin records and the framework record in one database transaction. |

BepInEx bootstrap prompts for installation only after a complete database lookup
finds no matching record. An existing record makes bootstrap a no-op; it is not
a check that framework files are intact. Query, mapping, row-iteration, or
ambiguous-result errors stop bootstrap and the enclosing add command before
confirmation, package fetches, downloads, backups, or file and record changes.
Framework lookup failures retain the repository classification and underlying
cause through installation and update service errors.

Declining bootstrap cancels the entire add before mod metadata fetches, downloads,
or file and record changes. A completed bootstrap with cleanup warnings is ready
only after its framework record is saved: the command reports that warning once
and continues the requested mod add. An incomplete bootstrap or an uncertain
metadata write stops the add, even if its error contains a file-completion marker.

After dependency validation, installing or replacing a root mod can still
succeed before a dependency lookup, file operation, or metadata write fails.
These requests are not all-or-nothing operations; an existing root record does
not establish that its dependencies completed.

Bulk updates coordinate packages by namespace and name within the request. If a
root needs a newer release of another selected mod, that dependency uses the
replacement lifecycle immediately, before the next selected package is processed.
Its confirmed release is reused by later roots and its own bulk turn, so the
obsolete directory is retired once rather than left beside the new release.
That selected turn still processes the updated mod's retained, validated
direct-dependency plan; it does not reinstall the package. Already-current
selected dependencies are left unchanged. Dependencies outside the original
selection use ordinary installation and are not recursively expanded. This does
not change add or single-update dependency handling.

Initial and dependency installations write records only after file installation
completes, including when final backup cleanup reports a warning. An incomplete
file-install step does not delete pre-existing records, so a
dependency backup failure preserves its earlier record and files. A failed
metadata write can have an unknown commit outcome; the diagnostic identifies
the installed path and asks the user to verify database state before retrying.
It does not authorize deleting a pre-existing record.

Framework removal identifies plugin records by their stored installation paths
within the configured `BepInEx/plugins` tree, not by `FrameworkID`. This covers
new and legacy records whose framework association is zero or stale without a
schema migration, association backfill, or foreign-key setting change. Records
with empty paths or paths outside that tree are retained rather than assumed to
belong to the removed framework. Other framework records are not deleted.

Path comparisons normalize relative paths and `.`/`..` components but do not
resolve filesystem aliases. Warden has one configured server directory and no
per-server database identity; this reconciliation does not add multi-server or
path-alias tracking. Use a stable absolute server path and inspect both files
and metadata when changing it or the working directory used with relative paths.

Incomplete or rolled-back framework removal leaves metadata unchanged.
Completed file removal still reconciles metadata when backup cleanup fails.
If metadata deletion fails, the service reports that the files were removed
and asks the user to verify database state before retrying. Plugin and framework
deletions share a transaction, and errors retain the write, commit, or rollback
cause. A failed call is not proof that no metadata changed. The consumed server
backup is not used to undo a completed removal after a database failure.

## Mod replacement and recovery

Single, namespace-qualified, and bulk updates validate the installation target
before downloading a replacement or changing the original. Each replacement is
downloaded, closed, extracted, and checked using the archive rules below in a
private staging directory beside plugins. The downloaded archive is removed
before publication. Preparation failures leave the original files and complete
record unchanged; any failed staging cleanup is reported separately.

After preparation, Warden renames the original release directory into staging,
then publishes the prepared release. If publication fails, it renames the
original back without overwriting a conflicting destination. Successful
restoration preserves the original directory and file permissions; a subsequent
cleanup error is not a failed restoration. Failed recovery reports the retained
original and prepared replacement paths. Keep those trees until the original
has been recovered and inspected.

The replacement owns its original independently of the file manager's shared
backup. Staging is outside plugins, so nested installations cannot overwrite or
consume it. Unrelated plugins and server files are not moved or restored.
Staging and publication use local renames; a cross-filesystem rename fails
rather than falling back to destructive copying. The plugins root is not
replaced, and its existing root-symlink preflight policy is unchanged. An
already-present replacement directory is refused rather than merged or
overwritten. A missing original directory can still be replaced.

After publication, the service writes the replacement record. Only a confirmed
write authorizes `ModReplacement.Commit` to discard the original. A failed
write may have committed: Warden leaves the replacement installed, retains the
original files, and neither deletes the record nor attempts a filesystem
rollback. The diagnostic includes the installed path, the original recovery
path (or its prior absence), and the complete original record for reference.
That record is reported, not saved as a database backup. Verify both installed
files and database state before retrying or restoring anything.

When the original directory was absent, the now-empty preparation directory is
cleaned after publication, before metadata handling. A failed cleanup returns the
published handle with a direct `ModInstallCleanupError` and the leftover path;
metadata must still be written. The handle has no original recovery to finalize,
and a later metadata failure does not leave unreported empty staging. This early
cleanup never applies to a handle containing original files.

Final cleanup consumes the recovery handle before deleting staging. A cleanup
failure after confirmed publication and recording is a completed file operation
with a direct `ModInstallCleanupError`, not permission to roll back. Leftovers
may be partially deleted and are not advertised as usable recovery snapshots.
The service retains the warning and continues direct dependencies and subsequent
bulk packages. A later failure still makes the whole request incomplete without
undoing earlier recorded replacements or dependencies.

This is per-package, in-process recovery, not a filesystem/database transaction
or a crash-atomic or concurrent-tree guarantee. Stop the server and avoid
concurrent filesystem changes while updating.

## Archive extraction

Mod and framework extraction creates missing parent directories, including
when an archive omits directory entries. It rejects absolute paths, relative
paths escaping the destination, and existing symlinks that resolve outside the
destination or cannot be resolved. A symlinked destination is resolved first;
symlinks within it are allowed only when their targets remain inside that
resolved directory. Relative destinations retain their physical filesystem
meaning, including parent components, even when the shell's `PWD` is a symlink.

Extraction stops and returns an error on path, directory creation, entry read,
checksum, file write, or close failures. Earlier entries and partially written
files can remain; extraction itself does not roll them back. The path checks
assume the destination tree is not modified concurrently and are not a
filesystem sandbox.

## Confirmations and backups

Confirmation inputs are case-sensitive. Ordinary prompts accept `Y` or `n`;
removing all mods or BepInEx uses `YES I AM` or `no`. Each prompt allows two
answers. Two invalid answers return `ErrMaxAttempts` without scanning a third
answer. EOF before an accepted or declined answer and scanner/reader failures
return `ErrConfirmationInput`, preserving the underlying cause (including
`io.EOF`) for `errors.Is`. These errors stop the operation before file or record
mutations, and commands display the input diagnostic. An answer delivered with
a non-EOF reader error does not authorize work; a valid final answer without a
newline is still accepted.

Mutation services distinguish deliberate cancellation and known no-work outcomes
from completion without changing their error-based APIs:

| Direct service result | Meaning |
| --- | --- |
| `nil` | The requested service operation completed without cleanup warnings. |
| `ErrCancelled` | A deliberate negative confirmation stopped the operation before its mutation or dependent work. |
| `ErrNoChanges` | A framework record already exists, or an update found no newer release. Bulk updates also use this result for an empty installed-mod list. |
| `ErrModAlreadyInstalled` | An add verified the installed root's dependency plan and found no missing direct dependencies. A completed dependency repair instead returns `nil` or the completed-request cleanup result below. |

Commands do not print mutation-success messages for cancellation or no-change.
Bootstrap's `ErrNoChanges` establishes readiness for the requested add; it does
not claim that framework files were checked. A bulk update returns no-change
only if none of its packages needed updating, not when some completed and others
were already current.

No-work outcomes must be direct service results, not nested causes of a failure.
EOF, reader failures (even alongside a negative answer), exhausted invalid input,
missing targets, lookup failures, and invalid versions remain failures. Wrapped
causes retain `errors.Is`/`errors.As` compatibility for diagnosis; finding a nested
outcome does not authorize continuation or declare the enclosing request complete.
Commands propagate these failures to a nonzero process exit while preserving
their diagnostic causes. Direct no-work outcomes and completed-request cleanup
warnings remain nonfatal; see the [command exit policy](../README.md#usage).

The file manager makes a backup available for restoration only after the
complete copy succeeds. After a failed copy, it attempts to remove the temporary
directory and returns any cleanup error alongside the creation error. A failed
attempt leaves any previous valid snapshot active. Mod and framework operations
stop if a required backup cannot be created rather than continuing with an
incomplete snapshot.

An empty source directory is valid for a backup; a missing source is an error.
Mod installation and removal of all mods create a missing plugins directory
before taking a backup. File-only removal of an absent release directory remains
a no-op; service removal of an unrecorded mod still returns `ErrModNotInstalled`.

Mod installation returns the original archive-write, extraction, or archive-delete
failure together with any restoration or restoration-cleanup failure. Failed
restoration reports the active snapshot's path when it remains available.
Successful restoration followed by cleanup failure restores that attempt's
snapshot, but the installation still failed; consumed cleanup artifacts are not
retryable snapshots. Mod replacement uses its independently owned original tree
rather than this standalone installation snapshot.

Once mod files are installed and the downloaded archive is removed, final backup
cleanup cannot undo that publication. The file manager returns the installed
path and a direct `ModInstallCleanupError` for this outcome. The mod service
still writes the record and installs remaining direct dependencies. The warning
does not authorize record deletion.

The service returns `ModMutationCleanupError` directly only after the entire
add or update request finishes, including metadata and direct dependencies
(and every selected mod for a bulk update). A later file, metadata, dependency,
package lookup, version, or dependency-validation failure keeps earlier cleanup
diagnostics but makes the request incomplete. Nested completion markers are
causes, not evidence that the enclosing request completed. Mod installation,
mod removal, and framework file-completion markers are not interchangeable.

Add and update commands display these diagnostics once. A completed request with
cleanup warnings is distinguished from an incomplete request; metadata failures
and unknown commit outcomes are not relabeled as completion. Earlier installed
files and records are not rolled back when later work fails.

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
receive `file.ModRemovalCleanupError` directly for this committed outcome. The
service returns `service.ModRemovalCleanupError` directly only after record
deletion and committed file removal; it retains `ErrUnableToRemoveMod` and the
file-layer causes for diagnostic compatibility. Neither a file-only marker nor
a nested service marker establishes completed service removal. This is not
installation completion. Updates use the owned replacement lifecycle instead
of committing a file-only removal before installation.

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

Successful publication consumes the active backup before cleanup, even if
cleanup fails. Explicit backup removal also consumes its handle before deleting
files. Cleanup errors identify leftover paths; a partially deleted snapshot is
not offered for another restore. The restored destination remains available.
Missing destinations and parents are supported, but restoration rejects
non-directory or symlink targets, filesystem roots, working-directory
ancestors, and paths overlapping the backup, including resolved aliases.
Restoration needs space and write access in the destination's parent and a
destination that can be renamed. A mount point that cannot be renamed fails
without deleting its live contents. The rename/recovery sequence is not
crash-atomic and does not guard against concurrent filesystem changes.

Framework installation, update, and removal check the restore target before
changing live files; mod installation checks its plugins target. Run these
operations from outside the tree they would restore. A rejected target returns
a preflight error without downloading, creating a backup, or mutating files.
Staged mod removal does not use general backup restoration and remains available
from inside the plugins directory: its rollback renames the original files back.
These checks do not guarantee that later I/O or cleanup will succeed.

A framework update owns one original-server snapshot throughout removal,
installation, configuration preservation, and plugin transfer. These steps do
not replace or consume that snapshot. A reported removal, download,
installation, configuration-preservation, or incomplete plugin-transfer failure
triggers restoration of the original framework, plugins, configuration, and
unrelated server files. Configuration file/directory conflicts and read or copy
errors fail the update rather than discarding settings. If restoration fails
before publication, the file-layer error identifies the retained server
snapshot for recovery. Standalone framework installation and removal keep their
own guarded backups and report restoration and cleanup failures.

Before plugin transfer, the update copies the original `BepInEx/config` tree
from that snapshot into the replacement framework. Each file is copied into a
private directory beside its destination and renamed into place only after the
copy succeeds. Existing readers of a packaged default keep reading that default,
not the preserved user bytes. Staging is removed after publication or failure;
copy, publication, and staging-cleanup errors fail the update through its
original-server recovery path. Configuration directory modes are restored after
their children are published; temporary publication roots remain private.

Existing files win byte-for-byte over packaged defaults at the same relative
path; new, non-conflicting defaults remain. This includes `BepInEx.cfg`,
mod-specific and nested configuration, and files without a `.cfg` extension.
Missing or empty pre-update configuration is valid and leaves new defaults in
place. Existing file permissions and final configuration-directory modes are
preserved. Ownership and extended metadata are not preserved. Warden does not
merge values or migrate settings.
This policy applies only to framework updates and that tree, not loader or
startup files elsewhere in the server or Warden's home configuration/database.

Framework plugin staging keeps a complete copy outside the server tree, where
framework removal and whole-server restoration cannot erase it. This needs
space and write access in the server directory's parent. Copying preserves plugin
subdirectory permissions without broadening private snapshot or transfer roots.
A failed initial copy stops before framework removal and cleans up its partial
staging; the file-layer error reports any cleanup failure and the retained server
backup.
Once framework replacement starts, a failed update retains the
complete plugin snapshot and reports its path. If the replacement archive omits
the plugins directory, copy-back creates it. Plugin transfer copies into the
plugins directory before using local renames, preserving non-conflicting
bundled plugins and keeping the independent snapshot even after a partial
transfer. A successful transfer removes that snapshot and reports cleanup
failures rather than attempting another framework rollback. Once framework,
configuration, and plugin replacement is complete, the update finalizes its
server snapshot even if plugin cleanup fails. Cleanup errors identify leftover
artifacts without advertising consumed server snapshots as valid recovery
sources.

Completed framework installation, update, or removal with cleanup failures
returns `file.FrameworkMutationCleanupError` directly from the file manager.
The service still saves or deletes the matching framework record, reconciles
removed plugin records, and retains the cleanup diagnostics. Only after metadata
succeeds does it return `service.FrameworkMutationCleanupError` directly.
If a record write also fails, it reports both errors and asks the user to verify
database state; the commit outcome may be unknown. A marker nested in another
error remains a cause, not proof that the enclosing operation completed.
The add, update, and remove commands display completed-mutation diagnostics,
including artifact paths. Framework removal also displays metadata-failure
details and database-verification guidance without a cleanup marker. A completed
bootstrap warning permits the mod add to continue, not to claim that the mod
request completed. A later mod or dependency failure remains a failure; a
verified already-installed mod remains a no-work outcome. A cleanup error after
an incomplete or rolled-back operation does not authorize metadata changes.

Filesystem recovery does not roll back database writes or provide crash or
concurrency atomicity.

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
files are present. [Repeating an add](#retrying-an-add) can finish missing direct
dependencies, but there is no general filesystem reconciliation command. If
restoring a known-good snapshot, restore the corresponding server files and
metadata together rather than deleting the database to silence an error.

For development and regression scenarios, use the temporary fixtures and
isolated execution procedure in [Contributing](../CONTRIBUTING.md), never a
live server.
