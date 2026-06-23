# Design: Dual-Write Postgres↔Graph Reconciliation Under Generation Swaps

**Status:** IMPLEMENTED (proof slice for #3559).
**Parent:** #3502 (reliability bar).
**Related:** #3557 (dead-letter triage), reconciliation drift retraction metric
(`eshu_dp_reconciliation_drift_retractions_total`).

## Problem

Graph edges carry denormalized provenance — `confidence`, `generation_id`, and
`resolved_id` — where `resolved_id` points back to the Postgres
`resolved_relationships` primary key and `generation_id` names the generation
the edge was projected from. Postgres is the source of truth.

The Postgres generation swap and the graph edge projection are **not one atomic
transaction**:

- `reducer.ProcessPartitionOnce` retracts edges, writes edges, then marks
  intents complete in Postgres as three separate steps. The graph group write is
  atomic per `GroupExecutor.ExecuteGroup`, but the graph write and the Postgres
  intent-completion update are distinct transactions.
- The authoritative generation flips in Postgres independently
  (`shared_projection_acceptance.generation_id`, `relationship_generations.status`).

A partial failure across that boundary can therefore strand a denormalized edge:

- **Graph-behind** (Postgres ok / graph retract failed): Postgres swapped to a
  new authoritative generation but the graph still holds an edge stamped with the
  superseded generation.
- **Graph-ahead** (graph ok / Postgres swap failed or rolled back): the graph
  committed an edge for a generation Postgres never made authoritative.
- **Orphan resolved_id** (within-generation retire): the edge generation matches
  the authoritative generation, but its `resolved_id` was retired from Postgres
  and the edge was never retracted.

Before this change there was no detector that, given a generation swap, found a
graph edge whose denormalized `generation_id`/`resolved_id` no longer matched the
now-authoritative Postgres generation. The existing
`eshu_dp_reconciliation_drift_retractions_total` metric only counts retractions
performed by a forced full snapshot; it does not detect steady-state divergence.

## Consistency Model Proven

The swap is **not** atomic; the system instead converges by reconciliation. This
slice proves convergence rather than inventing an atomic two-phase commit across
Postgres and the graph (which the runtime does not provide). The reconciliation
pass:

1. Takes the authoritative Postgres view per acceptance unit
   (`AuthoritativePostgresGeneration`: authoritative `generation_id` plus the
   `resolved_id` set that generation legitimately contains, built from
   `shared_projection_acceptance` joined to `resolved_relationships`).
2. Takes the denormalized graph edge view (`GraphDenormalizedEdge`).
3. Classifies each edge: `in_sync`, `stale_generation`, or `orphan_resolved_id`
   (`ClassifyReconciliationDrift`, backend-neutral and side-effect free).
4. Names the acceptance units holding stranded edges
   (`ReconciliationReport.DriftedEdgeKeys`), which drive the existing repo-scoped
   `EdgeWriter.RetractEdges` path. Retract-then-reproject removes the stranded
   edge and re-adds only authoritative-generation rows, so a subsequent
   classification reports full convergence with zero stranded edges.

The classifier is the engine; convergence reuses the existing retract mechanism,
so no new graph-write contract or hot-path Cypher is added.

## Fixture Intent → Reducer Graph Truth → Query Truth

- Fixture intent: an `AuthoritativePostgresGeneration` for `repo-a` at `gen-2`
  with `resolved-2`.
- Reducer graph truth: the denormalized edge set on the graph.
- Agreement: `ClassifyReconciliationDrift` reports `in_sync` only when graph
  provenance equals Postgres truth; any mismatch is drift and is scheduled for
  retract. The convergence test runs the retract-then-write path and re-classifies
  to assert agreement (`Converged()` true, no `gen-1` edge survives).

## Partial-Failure Injection Results

`go/internal/storage/cypher/generation_reconciliation_test.go` and
`generation_reconciliation_converge_test.go`:

- Graph-behind stale generation → `stale_generation`, edge scheduled for retract.
- Graph-ahead stale generation → `stale_generation`, not reported converged.
- Within-generation `resolved_id` retire → `orphan_resolved_id`, needs retract.
- Retired acceptance unit (no authoritative Postgres view) → `stale_generation`.
- Convergence: after retract-then-write over the named unit, re-classification
  reports `Converged()` with no surviving stale edge.
- Drift drives the real `EdgeWriter.RetractEdges` repo-scoped canonical retract
  anchored on the affected repo id.

## Operator Surface

- Metric: `eshu_dp_reconciliation_convergence_total`, bounded labels `domain`
  and `drift_kind` (`in_sync` / `stale_generation` / `orphan_resolved_id`).
  In-sync is recorded too, so an operator confirms a pass ran and converged
  rather than inferring health from metric absence.
- Log: `LogReconciliationReport` emits one structured line per pass — info on
  convergence, warn (alert-surfacing) when stranded edges are found, with a
  bounded per-domain sample of drifted edge keys.

## Evidence

No-Regression Evidence: the change adds a backend-neutral pure-Go classifier
(`ClassifyReconciliationDrift`), one Int64 counter, and one log emitter. It adds
no hot-path Cypher, graph-write, worker, lease, or batching change; the
`scripts/verify-performance-evidence.sh` gate reports "no hot
Cypher/concurrency/runtime files changed". Convergence reuses the existing
`EdgeWriter.RetractEdges` path unchanged, so the projection write path keeps its
current cost. Touched-package suites
`go test ./internal/storage/cypher ./internal/telemetry -count=1 -race`
pass (690 tests), classifier work is O(edges) over a prebuilt acceptance-unit
map (no graph scan).

Observability Evidence: `eshu_dp_reconciliation_convergence_total`
(`domain`, `drift_kind`) lets an operator query stranded-edge counts per drift
class; `LogReconciliationReport` emits a warn line naming a bounded sample of
drifted edge keys when convergence fails. Both are covered by
`generation_reconciliation_test.go` (metric values per drift_kind; warn vs info
log level; converged flag). Existing
`eshu_dp_reconciliation_drift_retractions_total` continues to count the actual
graph deletes performed when the convergence retract runs.
