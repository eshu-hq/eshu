# #8359 dedicated CONTAINS edge retract evidence

The semantic entity writer (`go/internal/storage/cypher/semantic_entity.go`)
retracted by `DETACH DELETE` on shared semantic nodes. #8359 replaces that
with a dedicated `[rel:CONTAINS]` edge retract
(`semantic_entity_edge_retract.go`): a stamped-edge statement, a
node-scoped statement for pre-stamp edges, and an edgeless-node reap, in
broad, label-scoped, delta, and canonical-node variants. Every containment
write template now stamps `rel.evidence_source`, which is the fence that
keeps the retract single-writer: the canonical node writer stamps its own
CONTAINS edges `projector/canonical` (`canonical_node_cypher.go`), so the
two writers' populations are disjoint by stamp plus endpoint labels.

## Accuracy proof

Live, on the pinned Neo4j (`neo4j:2026-community@sha256:eabfbb0…`, isolated
container, 2026-10-17):
`TestLiveSemanticContainmentRetractSparesForeignEdges` PASS (2.93s). It
seeds stamped, legacy-unstamped, canonical-stamped, foreign-type, and
foreign-repo edges on shared nodes, retracts through the real
`SemanticEntityWriter`, and proves: owned edges deleted (both stamp eras),
foreign `CALLS` and canonical `Class-[:CONTAINS]` edges survive, a node
holding a foreign edge is not reaped, an edgeless stale node is, and the
repo scope fences out the sibling repository.

The canonical-node mode — the dispatch production wires for NornicDB —
is proven on both pinned backends:
`TestLiveSemanticContainmentCanonicalRetractSparesForeignEdges` PASS on
Neo4j and on the pinned NornicDB (`fix-500-e022384c`, 1.3.3). That proof
forced a shape change: a label alternation inside a relationship pattern
matches nothing on that NornicDB image (measured: single labels match,
`File|ImplBlock` and `Function|Variable` alternations return zero rows),
so the label-scoped and canonical dispatches emit per-label
single-source statements; the broad alternation consts remain only in
the Neo4j broad mode, where they are live-proven above. The edgeless
reap's `NOT EXISTS { MATCH (n)-[]-() }` measured correct in both
directions on the same NornicDB image (edged node excluded, edgeless node
matched).

Hermetic: `TestSemanticContainmentEdgeRetractShape`,
`TestSemanticBroadRetractDeletesEdgesBeforeReapingNodes`, and
`TestSemanticCanonicalRetractRunsDedicatedEdgeRetractWithoutDetach` pin
the statement shapes and dispatch order for the broad, label-scoped, and
canonical-node modes.

## Performance Evidence

No-Regression Evidence: the retract anchor is unchanged — the semantic
label set plus `n.repo_id IN $repo_ids` (or `n.path IN $file_paths` for
delta) — so the matched node population per statement is the same set the
old `DETACH DELETE` scanned. The new work per matched node is its incoming
CONTAINS adjacency (degree 1–2: File containment, plus ImplBlock ownership
for Rust functions) and, in the reap, one adjacency existence check. The
write side adds `SET rel.evidence_source` after the existing endpoint-only
MERGE, the same SET the canonical writer already pays on its CONTAINS
edges. Statement count grows (broad 1→3, label-scoped 11→33) over the same
anchor population; no new label scan class, index, or transaction shape is
introduced, and retract statements remain inside the grouped transaction
(#6176 dispatch tests unchanged and green). This is a correctness change
with no performance improvement claim: the minimum bar is no regression on
the same anchor, and the stop threshold is any measured retract-stage
regression above 10% on a scaled replay. Live `PROFILE` db-hits
before/after at repo scale are NOT_CHECKED in this change; run before the
family's live-matrix promotion:

```bash
ESHU_CYPHER_BOLT_DSN=neo4j://127.0.0.1:7687 ESHU_CYPHER_BOLT_DATABASE=neo4j \
  go test ./go/internal/storage/cypher/ \
  -run TestLiveSemanticContainmentRetractSparesForeignEdges -v
```

## Concurrency

The grouped-retry classifier keeps accepting the mixed retract+upsert
group: both edge statements keep the parameter-bounded shape, and the
edgeless reap is accepted through the narrow `isEdgelessNodeReapStatement`
exception (`edgeless_reap.go`) with abuse cases pinned terminal in
`retrying_executor_retract_group_test.go`.
`TestSemanticEntityWriterGroupedRetractConvergesOnCommitUniqueConflict`
stays green, so the #6176 replay-to-convergence contract holds.

No-Observability-Change: no metric, span, log key, or pipeline stage is
added or renamed; retract statements keep `OperationCanonicalRetract` and
their existing statement-summary keys.
