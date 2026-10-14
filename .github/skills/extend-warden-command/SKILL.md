---
name: extend-warden-command
description: Add or change Warden CLI commands, flags, argument validation, or command-visible behavior. Use when a task changes the Cobra interface or connects a service operation to the CLI.
---

# Extend a Warden command

## Load the relevant contracts

Read [command conventions](../../instructions/commands.instructions.md), the
[README](../../../README.md), and
[Contributing](../../../CONTRIBUTING.md). If the command mutates installations,
also read the [installation lifecycle](../../../docs/install-lifecycle.md) and
use the [lifecycle-change procedure](../change-install-lifecycle/SKILL.md).

## Procedure

1. Establish the user-visible outcome: command syntax, required inputs, output,
   errors, and whether confirmation is needed. Clarify an ambiguous contract
   before changing existing behavior.
2. Find the owning command constructor and service contract. Extend an existing
   operation when it already owns the behavior; do not add SQL, network calls,
   or file manipulation directly to the command callback.
3. Follow the change through service implementation, dependency injection,
   and affected mocks. Wire new top-level commands in `main.go` and subcommands
   on their parent. Use existing flag definitions where their semantics match.
4. Add focused scenarios for accepted and rejected input, service failures, and
   observable output or side effects. Use injected fakes; do not invoke `main`
   from command tests. If confirmation changes, cover cancellation, invalid
   responses, and exhausted input as well as the affirmative path.
5. Keep help text and README examples aligned with the implemented syntax.
   For configuration changes, also check key validation, persistence, and
   defaults. Update canonical references only when their contracts change.
6. Run the smallest relevant package checks, then the contributor guide's CI
   checks. Use its isolated CLI procedure for manual help/output checks. Do not
   use live downloads, the real home directory, or a running Valheim server.

## Completion

Confirm that the command is reachable, affected fakes implement the service
contract, failures are visible, and documented examples match the command.
Report any unresolved behavior rather than treating a nil service error as
proof that a requested mutation occurred.
