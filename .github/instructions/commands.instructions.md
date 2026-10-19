---
applyTo: "command/**/*.go"
---

# Command conventions

- Follow the `New...Command` constructor pattern with injected services.
  Register new top-level commands explicitly in `main.go`; attach subcommands
  to their owning command.
- Keep SQL, HTTP, and installation logic below the command layer. Reuse service
  operations rather than implementing a second workflow in a Cobra callback.
- Reuse shared flag definitions. Specify argument counts, required flags, and
  related-flag constraints using Cobra validation.
- Treat flags, help text, output, confirmation behavior, and error reporting as
  user-visible contracts. Do not change them incidentally while adding a
  command. Map failure causes with `errors.Is` and do not silently discard
  unexpected failures. Classify direct no-work and completion results according
  to the [lifecycle outcome contract](../../docs/install-lifecycle.md#confirmations-and-backups),
  rather than treating nested causes as the enclosing operation's outcome.
- Exercise command behavior with injected fakes, including rejected input and
  service failures. Do not call `main` or start a real server in command tests.
- Update the [README usage](../../README.md#usage) with supported syntax when
  commands change. Configuration changes also need consistency with the config
  loader, key validation, and saved values.
- Use the [command-change skill](../skills/extend-warden-command/SKILL.md) for
  the complete procedure and the contributor guide for safe manual execution.
