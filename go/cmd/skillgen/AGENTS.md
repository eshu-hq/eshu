# AGENTS.md — cmd/skillgen guidance for LLM assistants

## Read first

1. `go/cmd/skillgen/README.md` — purpose, flags, invariants.
2. `go/cmd/skillgen/main.go` — `run`, `gen`, `check`; the entire
   business logic delegates to `internal/extensions/skillgen`.
3. `go/internal/extensions/skillgen/README.md` — the package contract
   the command drives.

## Invariants this package enforces

- **Thin driver.** `main` only calls `run`. `run` parses flags, calls
  `skillgen.LoadCapabilities`, `skillgen.LoadFragments`, and
  `skillgen.RenderAll`, then dispatches on the subcommand. No render,
  drift, or frontmatter logic belongs here.
- **`gen` and `check` share inputs.** Both subcommands use the same
  fragments directory, expected root, and capabilities file; the
  output of `gen` is the only thing `check` is allowed to compare
  against.
- **`check` is the gate.** It fails when `CheckDrift` returns a
  non-empty slice. Do not weaken this; it is the build-time defense
  against the agent-facing surface drifting from the fragments.

## Common changes and how to scope them

- **Add a new subcommand** → add a case to the switch in `run`,
  document the subcommand, add a `main_test.go` case. Why: the
  switch is the only subcommand dispatch point.
- **Add a new flag** → add a flag variable, document the default in
  the flag's `Usage` string, update the README's Flags table, add a
  focused test. Why: the flag set is the only flag surface.
- **Change the exit code** → the `main` wrapper always exits 1 on
  error and 0 on success. Do not introduce a subcommand-specific
  exit code; CI and the S3 gate rely on the binary semantics.

## Failure modes and how to debug

- Symptom: `check` reports `content_mismatch` → run `gen` and commit
  the diff; the canonical reason is a fragment changed without a
  baseline regeneration.
- Symptom: `check` reports `missing` → run `gen`; the expected
  directory is missing a file the generator writes.
- Symptom: `gen` errors with `ErrFragmentMissingByteCitation` → a
  fragment is missing the `byte_citation` field; add it to the
  fragment file.
- Symptom: `-fragments` or `-expected` not found → run from the `go`
  module directory or pass absolute paths.

## What NOT to change without an ADR

- The subcommand names (`gen`, `check`) — CI and the S3 gate depend
  on them.
- The default flag values — the existing CI scripts rely on the
  default paths relative to the `go/` module directory.
