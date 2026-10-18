# Canonical crossplane package guidance

## Ownership

- This package owns Crossplane Claim -> XRD `SATISFIED_BY` edge rows, Cypher
  templates, the write-reason registry, and statement-free parts.
- The parent `cypher` package owns `Statement` wrapping, the writer API
  (`CrossplaneSatisfiedByEdgeWriter`), `Executor` dispatch, and the
  `CrossplaneRelationshipMaterializedEdgeTypes` forwarder.

## Invariants

- Keep Cypher templates byte-identical unless the task explicitly changes graph
  behavior and supplies the required backend and performance evidence.
- Keep the retract scoped to the edge's own `scope_id` plus this writer's
  `evidence_source`. Endpoint nodes are cross-generation canonical and carry
  no reducer `scope_id`; a node-scoped predicate is a silent no-op that leaks
  stale edges.
- Never `MERGE` (fabricate) endpoint nodes: two `MATCH`es precede the edge
  `MERGE`, so a missing Claim or XRD is a no-op that self-heals next
  generation.
- Keep `rel_type` out of the `MERGE` property map (static `SATISFIED_BY`
  token only) so NornicDB uses its relationship hot path.
- Dispatch retracts through sequential `Execute`, never `ExecuteGroup`
  (NornicDB managed-transaction DELETE under-applies).
- Return a copy from `MaterializedEdgeTypes`; callers must not mutate package
  state.
- Do not import the parent `cypher` package.
