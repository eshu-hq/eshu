# Package registry package guidance

## Ownership

- This package owns package-registry templates, row shaping, phase names,
  part ordering, and the identity lock set, as statement-free parts.
- The parent `cypher` package owns `Statement` wrapping, the writer API, and
  phase dispatch (including the deferred second `ExecuteGroup`). This package
  never imports the parent. Do not add a leaf-to-parent import (it is a
  cycle: the parent's adapter already imports this package).

## Invariants

- Keep Cypher templates byte-identical unless the task explicitly changes
  graph behavior and supplies the required backend and performance evidence.
- Keep part fields aligned with what the parent stamps: Cypher, unbached
  rows, label, phase. Batching and statement metadata stay in the parent.
- Keep `MERGE`-on-`uid` identity; no `Repository` matches or ownership edges.
- Keep edge parts runnable after the node phases commit (no same-transaction
  assumptions about multi-label node visibility).
- Keep lock acquisition in sorted-key order; every `Lock` needs its `Unlock`.
- Keep the registry-event version gate: events without a `version_id` anchor
  are dropped by the row builder, never written as orphan nodes.
- Do not import sibling leaves (`edge/*`, `canonical/*`, `cloud/*`,
  `semantic/*`).

## Evidence and change history

Behavior-preserving moves record their proof in
`docs/internal/evidence/6694-semantic-registry-leaf.md`. Add new dated
entries there for template or dispatch changes.
