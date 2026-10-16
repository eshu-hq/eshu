# Canonical inheritance package guidance

## Ownership

- This package owns the canonical Cypher templates and four-type registry for
  inheritance edges.
- The parent `cypher` package owns the retract builder because the shared
  `Statement` type and dispatch callers remain there.
- `edge/writer` owns label-scoped query construction and routes fallback writes
  through this package.

## Invariants

- Keep the registry's four relationship types aligned with the retract's
  alternation. The parent package test checks both directions.
- Registry reasons must contain one qualified `inheritance.<Template>` citation
  that resolves to a string constant in this package. The materialized package's
  reason-symbol test enforces this owning-package boundary.
- Return a copy from `MaterializedEdgeTypes`; callers must not mutate package
  state.
- Preserve parameterized UNWIND/MATCH/MERGE templates and their relationship
  identity. Cypher edits require the graph-truth and performance evidence in the
  root agent guidance.
