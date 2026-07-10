# AGENTS.md — internal/envregistry guidance for LLM assistants

## Read first

1. `README.md` — purpose, scope, exported surface, and maintenance steps.
2. `entries.go` — the code-owned variable declarations (the data).
3. `registry.go` — `Registry`, `Validate`, and the typo/suggestion logic.
4. `coverage_test.go` — the CI gate (`coreScanFiles`) that enforces coverage.

## Invariants this package enforces

- **The registry is the source of truth for core + collector config.** Do not
  claim broader coverage than the registry actually holds. If you add a variable
  to a file in `coreScanFiles`, you MUST add it to `entries.go` (core) or
  `entries_collectors.go` (collectors) or the coverage test fails.
- **Validation must not produce false errors.** Unknown out-of-scope variables
  (e.g. container-registry credential `ESHU_*_OCI_*` test vars) are silent in
  non-strict mode; only invalid values for known variables are errors. Keep it
  this way so `eshu config validate` stays trustworthy and noise-free.
- **The reference doc is generated, never hand-edited.** Regenerate with
  `bash scripts/generate-env-registry-doc.sh`.
  `docs/public/reference/env-registry.md` carries a "do not edit by hand" banner.

## Common changes and how to scope them

- **Add a core variable** → add an `Entry` to the right subsystem group in
  `entries.go` (accurate `Type`, `Default`, `Allowed` for enums, `Aliases`,
  `Deprecated`/`ReplacedBy`); regenerate the doc; run
  `go test ./internal/envregistry -count=1`.
- **Expand coverage to a new core config file** → add the file to
  `coreScanFiles` in `coverage_test.go`, then register every `ESHU_*` it reads.
- **Deprecate/rename a variable** → set `Deprecated: true` and `ReplacedBy` on
  the old entry, keep it as an alias of the new canonical name if the value
  feeds the same setting, and regenerate the doc.

## What NOT to change without coordination

- The generated doc path (`docs/public/reference/env-registry.md`) and its
  mkdocs nav entry — operator docs link to it.
- Validation severity semantics — turning unknown variables into errors by
  default would break `eshu config validate` for every deployment that sets
  out-of-scope collector variables.
