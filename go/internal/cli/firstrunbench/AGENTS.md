# AGENTS.md — go/internal/cli/firstrunbench guidance for LLM assistants

## Read first

1. `go/internal/cli/firstrunbench/README.md` — purpose, ownership boundary,
   exported surface, and the wire-mirror invariant.
2. `go/internal/cli/firstrunbench/doc.go` — the godoc contract.
3. `go/cmd/eshu/first_run_benchmark_cmd.go` — the cobra wrapper that resolves
   flags and streams, decodes through `firstrun.ParseEnvelope`, calls in here,
   and maps the verdict to an exit code.
4. `go/internal/cli/demo` (criteria.go, benchmark.go, benchmark_render.go,
   render.go) and its wrapper `go/cmd/eshu/demo_benchmark_cmd.go` — the second
   consumer. The demo family builds its criteria rows from this package's
   `CriterionName`/`CriterionStatus` vocabulary, types its envelope error as
   `EnvelopeError`, renders with `Marker`, and reads envelopes with
   `ReadEnvelope`. Removing or renaming any of those breaks a family that
   lives in a different directory.
5. `go/internal/cli/firstrun/envelope.go` — the canonical envelope contract
   (`Envelope`, `EnvelopeError`, `ParseEnvelope`) this package scores.

## Invariants this package enforces

- **Health-only rejection.** `Evaluate` fails the benchmark when no bounded
  query returned, truth metadata is missing, no source handle is cited,
  indexing is incomplete, or the envelope carries an error. Do not soften a
  required criterion or add a fallback that lets a health-only run pass.
- **One decode, owned by `firstrun`.** The evaluator consumes a
  `firstrun.Envelope` from `firstrun.ParseEnvelope`; there is no second
  decoder to drift. Do not reintroduce a local mirror of `Result`/`Step`/
  `Diagnostic` — a mistyped artifact must keep failing the one shared parse.
- **Not-measured over fabricated.** Optional criteria record
  `CriterionNotMeasured` for missing inputs and never fail the run by
  themselves.
- **No process wiring here.** No cobra flag, no environment read, no exit
  decision. `RenderVerdict` writes only to the `io.Writer` it is handed;
  `ReadEnvelope`'s only filesystem call is `os.ReadFile(path)`.

## Anti-patterns

- **Reintroducing a local envelope mirror.** The duplicated
  `Result`/`Step`/`Diagnostic` shapes existed only while the first-run family
  was still `package main`; the contract now lives in
  `internal/cli/firstrun`. Edit wire tags or field types there, next to the
  emitter, never in a copy here.
- **Adding a criterion without deciding Required.** A new required criterion
  changes what rejects a run; confirm against the issue #1772 contract before
  flipping the benchmark's pass/fail behavior.

## Common changes

- New criterion: add the `CriterionName` constant, its evaluator, its append
  in `Evaluate` (row order is stable and rendered), and both a positive and a
  negative test that drive the real evaluator.
- Wire field added to first-run's result: add it to `firstrun.Result` (and
  the wrapper's map-based emitter when it needs to emit it); this package
  picks it up through the shared decode with no change here.
