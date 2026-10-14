# Working on Warden

Warden is a Go CLI for managing mods on a Valheim dedicated server. Cobra owns
the command interface, Viper loads configuration, SQLite records installations,
and Thunderstore supplies packages. Linux is the primary target; platform
branches alone do not establish macOS or Windows support.

## Read the relevant context

- [README](../README.md): supported commands and configuration.
- [Contributing](../CONTRIBUTING.md): prerequisites, local checks, fixtures, and
  an isolated CLI smoke check. Use its commands rather than inventing setup.
- [Installation lifecycle](../docs/install-lifecycle.md): mod/framework identity,
  operation ordering, partial failures, and recovery considerations. Read this
  before changing installation, updates, removal, or persistence.

## Preserve the boundaries

`main.go` wires dependencies and registers commands. Commands handle Cobra input
and user-facing results; services coordinate operations. The file manager owns
installation files, repositories own SQL, and the API client adapts Thunderstore
responses. Extend the existing injected dependencies instead of bypassing them
or introducing a second implementation of the same operation.

Keep changes focused. Existing code is evidence of behavior, not a requirement
to copy a bug or refactor unrelated code to match new style guidance. Preserve
public contracts unless the requested change explicitly revises them.

## Work safely

Every CLI invocation, including `--help`, loads or creates configuration and
initializes the database before dispatch. Never use the developer's real home
or Valheim installation for a smoke check. Follow the isolated procedure in
the contributor guide.

Filesystem changes and database writes are not one atomic transaction. Do not
promise rollback, dependency resolution, or platform support beyond what the
implementation and relevant scenarios actually establish.

## Use task-specific procedures

- For CLI behavior changes, use
  [extend-warden-command](skills/extend-warden-command/SKILL.md).
- For installation lifecycle changes, use
  [change-install-lifecycle](skills/change-install-lifecycle/SKILL.md).

Keep common setup and domain facts in their linked guides. Instructions and
skills should reference those sources rather than maintain competing copies.
