---
applyTo: "**/*.go"
---

# Go conventions

- Use the Go version declared in `go.mod` as the compatibility baseline. Run
  `gofmt` on changed Go files and follow the checks in
  [Contributing](../../CONTRIBUTING.md#build-and-check).
- Preserve explicit startup and constructor injection. Required dependencies
  are constructor arguments; do not add globals or `init()` functions to hide
  their creation.
- Reuse existing interfaces and helpers. Keep new dependency interfaces small
  and consumer-owned, but do not relocate existing interfaces or change public
  constructor signatures solely to impose that convention.
- Use ordinary arguments or a configuration struct for simple construction.
  Functional options are appropriate for several independent optional settings,
  not for introducing an unused extension point.
- Preserve exported sentinel-error compatibility with `errors.Is` or
  `errors.As`. Return errors from lower-level operations and report them at the
  relevant boundary; do not silently ignore a newly introduced failure or log
  and return the same error unnecessarily.
- Treat mutable inputs as caller-owned unless the API explicitly documents
  otherwise. Avoid retaining or returning mutable aliases unintentionally.
- Keep comments concise and current. Explain non-obvious intent or constraints,
  not the mechanics of the following statement or the history of the change.
- Do not hand-edit generated code if generation is introduced; change its
  source and regenerate. Do not add a new dependency or abstraction when the
  standard library or an existing project helper is sufficient.
