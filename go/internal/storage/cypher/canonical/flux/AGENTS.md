# Canonical flux package guidance

## Ownership

- This package owns the canonical Cypher templates, the T1-T4 Go resolution,
  and the one-type registry for Flux `RECONCILES_FROM` edges.
- The parent `cypher` package owns the `Statement` wrap
  (`flux_statements.go`) because the shared `Statement` type and
  the phase dispatch callers remain there.
- `querycontract` reads this package's registry directly for the
  edge-materialization coverage gate.

## Invariants

- Keep templates byte-identical to the reviewed move: one static template
  per (reconciler, target label) pair, never a data-driven label.
- `EdgeParts` returns retract parts before upsert parts. The parent wrap
  preserves order; the root order test pins both the family position and
  the retract-before-upsert sequence.
- Return a copy from `MaterializedEdgeTypes`; callers must not mutate package
  state.
- Preserve parameterized UNWIND/MATCH/MERGE templates and their relationship
  identity. Cypher edits require the graph-truth and performance evidence in
  the root agent guidance.
- `metadataString` is a leaf-local copy of the parent helper: never import
  the parent package from this leaf.
