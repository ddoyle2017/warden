---
applyTo: "**/*_test.go,internal/test/**/*.go"
---

# Test conventions

Read [Tests and fixtures](../../CONTRIBUTING.md#tests-and-fixtures) before
creating or restructuring tests.

- Prefer the standard `testing` package and external `package name_test`
  packages, exercising public behavior rather than private implementation.
- Use table-driven cases when setup and assertions are shared. Name new cases
  as `given <context>, when <action>, then <outcome>`; omit `given` when it adds
  nothing. Existing `_Happy` and `_Sad` function names are not a requirement to
  rename unrelated tests.
- Check both the expected error state and the named result: returned values,
  persisted records, remaining files, or significant interactions. A success
  assertion alone does not prove an update, removal, or cancellation happened.
  Use `errors.Is` or `errors.As`; compare error text only when it is contractual.
- Use the existing function-field mocks. Configure callbacks the scenario
  reaches, and assert behaviorally significant arguments and effects. Do not
  add success-shaped defaults to conceal unexpected mock calls.
- Create a fresh helper inside each stateful subtest. Its database and server
  directories are temporary; checked-in archives and scripts are read-only
  input. Use `t.TempDir()` for other writable fixtures and `t.Cleanup` for
  resources. Setup failures that invalidate assertions must stop that case.
- Reset global Viper state before and after configuration cases. Do not add
  `t.Parallel()` around global state, shared readers, or shared fixtures.
  Parallelize only independently owned state.
- Make failure cases deterministic: use a missing table, a controlled mock
  error, or a regular file where a directory is required. Do not rely on an
  arbitrary absolute path, permission differences, network access, or sleeps.
- Never use a real home directory, game server, or live mod download as a test
  fixture. Do not skip a scenario merely because a dependency is hard-wired.
