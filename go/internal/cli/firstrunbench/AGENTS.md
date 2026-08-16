# AGENTS.md — go/internal/cli/firstrunbench guidance for LLM assistants

## Read first

1. `go/internal/cli/firstrunbench/README.md` — purpose, ownership boundary,
   exported surface, and the wire-mirror invariant.
2. `go/internal/cli/firstrunbench/doc.go` — the godoc contract.
3. `go/cmd/eshu/first_run_benchmark_cmd.go` — the cobra wrapper that resolves
   flags and streams, calls in here, and maps the verdict to an exit code. It
   also keeps the package-main `firstRunEnvelope` (typed against
   `firstRunResult`) for the first-run-evidence family, plus the alias seam
   the demo-benchmark family compiles against.
4. `go/cmd/eshu/demo_benchmark.go` and `demo_benchmark_cmd.go` — the second
   consumer. The demo family builds its own criteria rows from this package's
   `CriterionName`/`CriterionStatus` vocabulary and renders with `Marker`; it
   reads envelopes with `ReadEnvelope` and its test constructs
   `EnvelopeError`. Removing or renaming any of those breaks a family that
   lives in a different directory.
5. `go/cmd/eshu/first_run_benchmark_cmd_test.go` — the wire-parity test that
   pins `Envelope`/`Result`/`Step`/`Diagnostic` to the canonical envelope
   shape in `package main`.

## Invariants this package enforces

- **Health-only rejection.** `Evaluate` fails the benchmark when no bounded
  query returned, truth metadata is missing, no source handle is cited,
  indexing is incomplete, or the envelope carries an error. Do not soften a
  required criterion or add a fallback that lets a health-only run pass.
- **Decode strictness mirrors the canonical envelope.** `Result` carries
  fields the evaluator never reads (`Command`, `RuntimeShape`, `Diagnostic`,
  …) on purpose: a mistyped block anywhere in the artifact must fail
  `ParseEnvelope` exactly as it fails the package-main decoder. Slimming the
  struct silently accepts corrupt artifacts.
- **Not-measured over fabricated.** Optional criteria record
  `CriterionNotMeasured` for missing inputs and never fail the run by
  themselves.
- **No process wiring here.** No cobra flag, no environment read, no exit
  decision. `RenderVerdict` writes only to the `io.Writer` it is handed;
  `ReadEnvelope`'s only filesystem call is `os.ReadFile(path)`.

## Anti-patterns

- **Importing `n` from anywhere, or exporting it.** It is a verbatim copy of
  the placeholder helper in `go/cmd/eshu/first_run.go`, kept local because
  `package main` cannot be imported.
- **Editing wire tags or field types on one side only.** Change the mirror
  and the package-main envelope together, or the wire-parity test goes red.
- **Adding a criterion without deciding Required.** A new required criterion
  changes what rejects a run; confirm against the issue #1772 contract before
  flipping the benchmark's pass/fail behavior.

## Common changes

- New criterion: add the `CriterionName` constant, its evaluator, its append
  in `Evaluate` (row order is stable and rendered), and both a positive and a
  negative test that drive the real evaluator.
- Wire field added to first-run's result: add it to `Result` here and to the
  package-main `firstRunEnvelope`'s `firstRunResult` side in the same change;
  the parity test enumerates the populated fields.
