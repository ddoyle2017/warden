---
applyTo: "internal/data/repo/**/*.go"
---

# Storage changes

- Repositories own SQL; keep file operations and Thunderstore calls outside
  this layer. Continue using injected database dependencies and parameterized
  values.
- Treat the existing `.warden.db` as persistent user data. `CREATE TABLE IF NOT
  EXISTS` initializes a missing table; it does not migrate an existing one.
  Schema changes require an explicit compatibility and migration decision,
  not instructions to delete the user's database.
- Keep schema definitions, selected columns, positional row scans, domain
  fields, and fixture data consistent. Prefer explicit selected columns when
  changing a query so mapping does not depend on unrelated schema ordering.
- Preserve the distinction between no rows, multiple rows, mapping failures,
  and query failures. Retain sentinel-error compatibility for service callers.
- Execute statements that belong to a transaction on that transaction. Handle
  commit and rollback failures deliberately; exercise the corresponding
  failure paths when changing transaction behavior.
- Use fresh temporary SQLite databases for persistence behavior and malformed
  schema cases. Use a complete function-field mock for deliberately injected
  database failures; do not bypass transaction setup just to reach an assertion.
- Review the [lifecycle reference](../../docs/install-lifecycle.md) when a
  storage change affects installed files or mod/framework identity. Do not
  assume database transactions also roll back the filesystem.
