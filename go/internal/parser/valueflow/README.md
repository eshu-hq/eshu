# Value-Flow Bridge

## Purpose

`valueflow` composes the four value-flow engines into one end-to-end
interprocedural taint pipeline. It derives a function's TITO summary from its
control-flow graph and intraprocedural taint facts, feeds the incremental
summary store, and assembles the interprocedural port graph. It is the first
integration slice of the value-flow taint engine (epic #2705, issue #2823):
parse → per-function CFG + taint → summary effects → interprocedural program →
findings.

## Ownership boundary

This package owns the composition between the engines, not the engines. It does
NOT parse source or resolve calls — a per-language lowering supplies the
`EffectsSpec` (which statements are parameters, sources, sinks, sanitizers,
returns, and call-argument sites). It does not persist anything or run in the
reducer; those are later integration steps.

## Exported surface

See `doc.go` for the godoc contract. The surface is:

- `DeriveEffects(fn cfg.Function, spec EffectsSpec) summary.Effects` — derive a
  function's TITO summary by running the taint engine from its parameters and
  internal sources.
- `EffectsSpec`, `ParamSlot`, `SourceSlot`, `SinkSlot`, `CallArgSlot` — the
  per-statement annotation a lowering supplies.
- `BuildProgram(summaries, sources, sinks) interproc.Program` — assemble the
  interprocedural port graph from summaries.

## Dependencies

- `internal/parser/cfg` (the CFG input), `internal/parser/taint` (the
  intraprocedural propagation reused for derivation), `internal/parser/summary`
  (the effect model and store), `internal/parser/interproc` (the port graph and
  solver). Standard library `sort`.

## Telemetry

None. Pure composition; a reducer that drives the pipeline owns metrics.

## Gotchas / invariants

- **DeriveEffects reuses the taint engine.** It marks each parameter/source as a
  taint origin and each return/call-arg as a pseudo-sink (reserved NUL-prefixed
  kinds that cannot collide with a real kind or be neutralized). A parameter
  sanitized before a real sink yields a SANITIZES finding, so no `ParamToSink`
  effect — correct for composition.
- **A sanitizer is applied where it redefines a binding** (`safe = escape(arg)`),
  not where the value is used at a sink. Spec authors place sanitizers on the
  defining statement.
- **Determinism.** Derived effects are sorted and de-duplicated; `BuildProgram`
  iterates summaries in sorted ID order.

## Related docs

- Epic #2705, integration issue #2823. Consumes #2727 (cfg), #2728 (taint),
  #2729 (summary), #2730 (interproc).
- Remaining: the Go-AST-to-`EffectsSpec` extraction (call resolution), a
  Postgres-backed summary store, and reducer projection.
