# AGENTS.md - internal/parser/valueflow guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: how the four engines compose
3. `valueflow.go` - `DeriveEffects`, the `EffectsSpec` model, pseudo-sink
   encoding
4. `program.go` - `BuildProgram`, summary-to-port-graph mapping
5. `valueflow_test.go` - param->sink (with sanitizer), param->call-arg, and the
   full interprocedural pipeline proof
6. The engines this bridges: `../cfg`, `../taint`, `../summary`, `../interproc`

## Invariants this package enforces

- Composition only: no parsing, no call resolution, no persistence. A lowering
  supplies the `EffectsSpec`; the reducer drives persistence and projection.
- DeriveEffects reuses the taint engine — do not add a second propagation. The
  return and call-arg pseudo-sink kinds are NUL-prefixed so they cannot collide
  with a real sink kind or be neutralized by a sanitizer.
- Only `FindingTainted` flows become effects; `FindingSanitized` (a sanitized
  param) correctly yields no `ParamToSink`.
- Deterministic: derived effect lists are sorted and de-duplicated;
  `BuildProgram` iterates summaries in sorted ID order.

## Common changes and how to scope them

- Support a new effect kind: extend `summary.Effects` (in the summary package
  first), then map it in `DeriveEffects` and `BuildProgram`, with a test.
- Add the Go extraction: build an `EffectsSpec` from the Go AST (call resolution,
  argument binding) in the golang package; this package needs no change.

## Failure modes and how to debug

- A sanitized parameter still produces `ParamToSink`: the sanitizer is on the
  sink statement, not the statement that redefines the binding. Sanitizers apply
  where they redefine.
- Missing interprocedural finding: a `ParamToCallArg` effect did not resolve to a
  cross-function edge, or the callee port index is wrong. Dump the built Program.

## Do not change without review

- The pseudo-sink encoding (NUL-prefixed kinds) or the taint-engine-reuse
  approach; a second propagation would drift from the kind-set model.
