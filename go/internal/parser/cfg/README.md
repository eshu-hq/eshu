# Control-Flow Graph and Reaching Definitions

## Purpose

`cfg` builds a per-function control-flow graph and resolves reaching
definitions over it, emitting bounded, deterministic def->use facts. It is the
dataflow substrate for value-flow taint analysis (epic #2705): a use of a
binding is linked to every definition that can reach it across branches and loop
back-edges. The package is language neutral; a per-language lowering feeds it
basic blocks and statements.

## Ownership boundary

This package owns the graph data structures, the reaching-definitions fixpoint,
and the deterministic, bounded fact serialization. It does NOT parse source or
know any language grammar — lowering an AST into a `Builder` is the caller's job
(the Go lowering lives in `internal/parser/golang`). It does not decide what a
"source" or "sink" is; taint semantics are layered on top by later passes.

## Exported surface

See `doc.go` for the godoc contract. The surface is:

- `Builder` with `NewBuilder`, `AddBlock`, `SetEntry`, `AddStmt`, `AddEdge`, and
  `Build`. Callers construct one Builder per function.
- `Function`, `Block`, `Stmt`, `DefUse`, and `Overflow` — the resolved result.
- `Limits` and `DefaultLimits` — the caps that bound computation.

## Dependencies

Standard library only (`sort`). No graph backend, storage, or telemetry
dependencies; this is a pure computation package.

## Telemetry

None. The package emits no metrics, spans, or logs — it is a pure function from
Builder calls to a `Function`. Overflow is surfaced as data on the result
(`Function.Overflow`) for the caller to record, not logged here.

## Gotchas / invariants

- **Parameters are entry definitions.** Lowerings model function parameters as
  definitions in the entry block so parameter-to-use flow is captured. The entry
  block's in-set is always empty.
- **Uses see entry-of-statement reaching sets.** A statement that both uses and
  defines a binding (`x = x + 1`) observes the definitions reaching its entry,
  before its own definition applies.
- **Determinism is required, not incidental.** Successors and def->use edges are
  sorted; the fixpoint iterates blocks in ascending ID order. Do not introduce
  map iteration into emitted output.
- **Overflow is counted, never silent.** Tripping a cap records a count on
  `Overflow`; it never drops data without a signal. Language lowerers can also
  record field-sensitive access-path truncation through `Overflow.AccessPaths`
  when `Limits.MaxAccessPathParts` is exceeded.

## Related docs

- Epic #2705 (value-flow taint engine), child issue #2727 (this pass).
- `internal/parser/golang` lowering: `cfg_lower.go`, `cfg_bindings.go`,
  `cfg_emit.go`.
