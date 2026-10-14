---
name: change-install-lifecycle
description: Change Warden mod or BepInEx installation, updates, removal, dependencies, backups, or related persistence. Use when correctness depends on coordinating filesystem state, database records, or confirmation behavior.
---

# Change an installation lifecycle

## Load the relevant contracts

Read the [installation lifecycle](../../../docs/install-lifecycle.md),
[installation instructions](../../instructions/installation.instructions.md),
[storage instructions](../../instructions/storage.instructions.md), and
[test guidance](../../../CONTRIBUTING.md#tests-and-fixtures). Read the command
instructions too if flags, prompts, or user-facing results change.

## Procedure

1. Identify the operation and distinguish ordinary mods from framework files.
   Record its inputs and expected state on success and cancellation. Make
   namespace, package name, full release name, and dependency-version choices
   explicit wherever they affect the change.
2. Follow the relevant service, file-manager, and repository calls. List the
   fallible steps in order and what files and records remain after each failure.
   Check callers and dependency handling, not just the method being edited.
3. Identify who owns each backup and temporary directory, which step can
   overwrite it, and how cleanup and restoration errors reach the caller.
   Do not assume a SQL transaction or nested backup provides whole-operation
   rollback.
4. Implement the requested behavior without silently changing confirmation,
   dependency selection, schema compatibility, or unrelated installations.
   Preserve existing plugin contents and unrelated server files. If the task
   requires a new recovery contract, make that change explicit.
5. Use temporary SQLite databases and server directories for scenarios. Reuse
   checked-in archives or construct small synthetic archives locally; mock
   HTTP and input. Cover the changed operation's success and partial failures,
   plus cancellation and exhausted input when interactive behavior changes.
   Assert persisted records, remaining files and contents, and meaningful
   dependency calls rather than only nil/non-nil errors.
6. Run affected package checks and the contributor guide's CI checks. Do not
   download mods, run a real server, or use arbitrary external filesystem
   paths to provoke failures.
7. Update the lifecycle reference if operation ordering, identity, or failure
   semantics changed. Update README usage only if user-facing behavior changed;
   do not copy the lifecycle table into this skill.

## Completion

State which observable outcomes are established and any remaining limitation.
Do not claim atomicity, rollback, or platform compatibility solely because an
operation returned nil or a backup helper was called.
