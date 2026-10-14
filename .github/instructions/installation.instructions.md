---
applyTo: "internal/service/**/*.go,internal/data/file/**/*.go"
---

# Installation changes

Read the [installation lifecycle](../../docs/install-lifecycle.md) before
changing these operations. Use the
[lifecycle-change skill](../skills/change-install-lifecycle/SKILL.md) for work
that crosses service, filesystem, and persistence boundaries.

- Keep ordinary mods and frameworks distinct. Their destination layouts and
  removal scopes differ; framework updates must account for existing plugins.
- Distinguish package name, namespace, and versioned full name at each boundary.
  Review dependency handling when changing any install or update path; do not
  silently change how dependency versions or BepInEx are selected.
- Preserve confirmation behavior unless the task explicitly changes it.
  Exercise affirmative, negative, invalid, and exhausted-input paths when
  changing interactive control flow. A nil error does not necessarily mean a
  mutation happened.
- Identify filesystem and database state after each fallible step. Preserve
  unrelated files and records, and test partial failures rather than assuming
  a backup makes the entire operation atomic.
- Handle backup, restore, and cleanup errors explicitly in new or changed
  control flow. Nested operations must not unintentionally replace the backup
  needed by their caller.
- Use platform-aware paths and review containment whenever joining
  user-supplied or archive paths. Do not assume an existing helper establishes
  safety properties it does not enforce.
- Keep test operations inside temporary, owned directories. Use checked-in or
  synthetic fixtures instead of downloading mods or launching Valheim.
