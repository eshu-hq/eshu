# Semantic entity package guidance

## Ownership

- This package owns the semantic-entity writer, its Cypher templates, row
  shaping, retract dispatch, and grouped-transaction splitting.
- The parent `cypher` package owns the `Executor`/`Statement` seam. This
  package imports the parent as `sourcecypher`; the parent never imports this
  package. Do not add a parent-to-leaf import (it is a cycle).

## Invariants

- Keep Cypher templates byte-identical unless the task explicitly changes
  graph behavior and supplies the required backend and performance evidence.
- Keep retracts leading upserts in statement order.
- Keep `WrapRetryableNeo4jError` on every writer error path.
- Keep the generation fence (`writer_fence.go`) on every dispatch: fence
  before every group including the first and retract-only writes, on both
  the grouped and the per-statement fallback path; fail closed on probe
  errors and unstamped stale verdicts; never let a fence exit bypass the
  partial-result + `ErrGenerationSuperseded` contract.
- Keep `MERGE`-on-identity (never `CREATE`) on every write path.
- Keep test fakes local: duplicate what the leaf needs, never import the
  parent's test helpers (they are unimportable across the split).
- Do not import sibling leaves (`edge/*`, `canonical/*`, `cloud/*`).

## Evidence and change history

Behavior-preserving moves record their proof in
`docs/internal/evidence/6694-semantic-registry-leaf.md`. Add new dated
entries there for template or dispatch changes.
