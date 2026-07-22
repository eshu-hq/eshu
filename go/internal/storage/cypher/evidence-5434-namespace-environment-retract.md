# Evidence: KubernetesNamespace TARGETS_ENVIRONMENT stale-edge retract and node-property clear (codex review finding P1, #5434)

## Problem

`KubernetesNamespaceNodeWriter.WriteKubernetesNamespaceNodes`
(`kubernetes_namespace_node_writer.go`) MERGEs a
`(:KubernetesNamespace)-[:TARGETS_ENVIRONMENT]->(:Environment)` edge for
environment-bound rows and REMOVEs node properties (never the edge) for
unbound rows, but never retracted a PRE-EXISTING `TARGETS_ENVIRONMENT` edge:

- a namespace that was bound (e.g. `prod`) and then lost its recognized
  environment label kept the stale edge, so the graph kept asserting the old
  environment forever;
- a namespace re-bound from one environment to another (e.g. `prod` ->
  `stage`) accumulated a SECOND edge instead of replacing the first, since
  `MERGE` only matches an edge to the SAME target `Environment` node.

A second, related defect surfaced while proving the edge-retract fix live:
`canonicalKubernetesNamespaceUpsertCypher`'s trailing `REMOVE n.environment,
n.evidence_class` (the clause responsible for clearing a namespace's
node-property environment truth on a bound->unbound transition) is #5434's
own code, introduced in the same original PR (`f6ae0d0742`), and fails
outright on the pinned NornicDB v1.1.11 when dispatched through the real
production managed-transaction path (`ExecuteGroup`) -- so a bound->unbound
transition's node-property clear never even ran, and the whole write errored.

## Fix

Added `retractKubernetesNamespaceStaleTargetsEnvironmentCypher`, a single
statement covering every row in the batch (bound and unbound alike):

```cypher
UNWIND $rows AS row
MATCH (n:KubernetesNamespace {uid: row.uid})-[rel:TARGETS_ENVIRONMENT]->(old_env:Environment)
WHERE rel.evidence_source = $evidence_source
  AND old_env.name <> row.environment
DELETE rel
```

`old_env.name <> row.environment` covers both transitions with one statement:
for an unbound row (`row.environment == ""`) a real `Environment` node's name
is never the empty string, so the predicate is unconditionally true and any
existing edge is deleted; for a bound row it is true only when the
namespace's environment actually changed, so the steady-state case (unchanged
binding) matches nothing and never deletes+recreates its own edge. Scoped by
`uid` (the writer's own MERGE identity) and `evidence_source` (this writer's
own edges only).

`WriteKubernetesNamespaceNodes` now runs this retract via a new
`dispatchRetract` helper -- **sequential `Execute` calls, NEVER
`ExecuteGroup`** -- before building the upsert statements, mirroring
`AzureCloudResourceEdgeWriter.dispatchRetract`
(`evidence-4367-cloud-edge-retract.md`). This is required, not stylistic: on
the pinned NornicDB v1.1.11, `cmd/reducer` wires this writer's executor
through `reducerNeo4jExecutor.ExecuteGroup` -> `RetryingExecutor.ExecuteGroup`
-> `cypherRunnerStatementExecutor.ExecuteGroup` ->
`neo4jSessionRunner.RunCypherGroup`, which runs every statement in ONE
managed Bolt transaction (`session.ExecuteWrite`) unconditionally -- this
writer's executor never routes through the NornicDB `storagenornicdb.
PhaseGroupExecutor`/`Drain` mechanism the offline canonical projector
(`CanonicalNodeWriter`, `structural_edges` phase) uses, so a `Drain` field on
the retract statement would be silently inert here. Probed directly (see
below): a relationship `DELETE` dispatched through `ExecuteGroup` can
under-apply even as the sole statement in the group, while the identical
statement run auto-commit (`Execute`) deletes correctly -- the same class of
defect the cloud-correlation writers already fixed.

Separately, `canonicalKubernetesNamespaceUpsertCypher`'s trailing `REMOVE
n.environment, n.evidence_class` is replaced with `SET n.environment = null,
n.evidence_class = null`. `SET prop = null` is the openCypher-standard
property-delete form and, unlike `REMOVE`, was proven live (see below) to
apply correctly under the exact same `ExecuteGroup` managed-transaction path
-- consistent with every OTHER property clear in this writer already using
`SET`, never `REMOVE`.

## Probe table

Probed directly against a lean `timothyswt/nornicdb-cpu-bge:v1.1.11`
container (same image/digest as `scripts/verify-replay-tier.sh`) before
finalizing the dispatch mechanism, using a minimal Go program against the
real Neo4j Bolt driver:

| Probe | Dispatch mechanism | Result |
| --- | --- | --- |
| `UNWIND $rows AS row MERGE (n:KubernetesNamespace {uid: row.uid}) SET n.id = row.uid REMOVE n.environment, n.evidence_class` | `session.Run` (autocommit) | OK |
| identical statement | `session.ExecuteWrite` (managed transaction, `tx.Run`) | `Neo4jError: Neo.ClientError.Statement.SyntaxError (REMOVE requires a MATCH clause first ...)` |
| retract shape via HTTP `tx/commit` (single statement, full row) | HTTP autocommit | OK |
| retract shape via HTTP `tx/commit` (two sequential autocommit calls: bound-write then unbound-write against the same uid) | HTTP autocommit x2 | OK, both applied |
| `UNWIND $rows AS row MERGE (n:KubernetesNamespace {uid: row.uid}) SET n.id = row.uid, n.environment = null, n.evidence_class = null` | `session.ExecuteWrite` (managed transaction, `tx.Run`) | OK -- proven live end to end below, including read-back of the cleared node properties |

**Conclusion:** `canonicalKubernetesNamespaceUpsertCypher`'s bare `REMOVE`
fails specifically when dispatched through the Bolt driver's
managed-transaction `ExecuteWrite` -- the SAME production dispatch path this
writer's `ExecuteGroup` upsert always uses. Since this statement is #5434's
own code (introduced in the original PR, `f6ae0d0742`), it is fixed here
rather than deferred: replacing `REMOVE n.environment, n.evidence_class` with
`SET n.environment = null, n.evidence_class = null` was proven live to apply
correctly under the identical managed-transaction path (see
`TestReducerKubernetesNamespaceEnvironmentRetractGraphTruth/bound_to_unbound_removes_the_edge_and_clears_node_properties`
below). The retract statement added for the edge-stale fix (no `REMOVE`
clause; a plain `MATCH ... WHERE ... DELETE rel`) was never affected by this
defect, and its own dispatch mechanism (sequential autocommit `Execute`,
never `ExecuteGroup`) was independently proven correct by the existing
`evidence-4367-cloud-edge-retract.md` probe for the identical shape-class.

## Benchmark Evidence:

Failing-then-green live regression on the pinned production backend
(behavior change -- the old writer never retracted a stale edge):

```bash
cd go && ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_DATABASE=nornic \
NEO4J_URI=bolt://localhost:<port> NEO4J_USERNAME=neo4j NEO4J_PASSWORD=change-me \
go test ./internal/replay/offlinetier/ -run TestReducerKubernetesNamespaceEnvironmentRetractGraphTruth -count=1 -v
```

**Edge retract (retractKubernetesNamespaceStaleTargetsEnvironmentCypher /
dispatchRetract):**

- RED (retract dispatch temporarily disabled to reproduce the pre-fix
  writer):
  - `bound_to_unbound_removes_the_edge_and_clears_node_properties`:
    `TARGETS_ENVIRONMENT edges = [replay-ns-env-retract:prod], want []` --
    the stale edge survives.
  - `bound_prod_to_bound_stage_leaves_exactly_one_edge_pointing_at_stage`:
    `TARGETS_ENVIRONMENT edges = [replay-ns-env-retract:prod], want
    [replay-ns-env-retract:stage]` -- the stale `prod` edge survives
    alongside (before) the new `stage` edge.
- GREEN: `ok github.com/eshu-hq/eshu/go/internal/replay/offlinetier` after
  restoring `dispatchRetract`. Both subtests pass: the bound->unbound
  transition leaves zero edges, and the prod->stage transition leaves
  exactly one edge, pointing at stage.

**Node-property clear (REMOVE -> SET ... = null in
canonicalKubernetesNamespaceUpsertCypher, codex review follow-up):**

- RED (Cypher temporarily reverted to the original `REMOVE n.environment,
  n.evidence_class`):
  - `bound_to_unbound_removes_the_edge_and_clears_node_properties`: the
    SECOND write itself fails --
    `second (unbound) write: execute write group: Neo4jError:
    Neo.ClientError.Statement.SyntaxError (REMOVE requires a MATCH clause
    first (e.g., MATCH (n) REMOVE n.property))` -- proving `REMOVE` does not
    merely leave the property stale, it breaks the entire write under the
    real production `ExecuteGroup` managed-transaction path.
- GREEN: `ok github.com/eshu-hq/eshu/go/internal/replay/offlinetier`, 3/3
  runs (`-count=3`, ~0.1s per subtest pair) after restoring `SET
  n.environment = null, n.evidence_class = null`. The write succeeds and
  `assertNamespaceNodeProperties` confirms, by direct read-back: after
  bound->unbound, `n.environment` and `n.evidence_class` are both
  null/absent and `n.environment_state = "environment-unbound"` (with a
  positive-control read after the first bound write confirming the query
  itself returns the bound values, so the negative assertion is trusted).

Companion writer-level unit proof (fast, no Docker, `go test
./internal/storage/cypher/... -run
'TestKubernetesNamespaceNodeWriterEmitsRetractBeforeUpsert|
TestKubernetesNamespaceNodeWriterTargetsEnvironmentTransitions|
TestKubernetesNamespaceNodeWriterRetractNeverGroups|
TestKubernetesNamespaceNodeWriterUnboundRowNeverCreatesEnvironment'
-count=1 -v`) drives the same production `WriteKubernetesNamespaceNodes` and
replays the recorded statements through an in-memory MATCH/DELETE/MERGE
edge-set model (`namespaceTargetsEnvironmentGraph`) to prove the same two
edge transitions, a `sqlSequentialRecordingExecutor`-backed guard
(`TestKubernetesNamespaceNodeWriterRetractNeverGroups`) proving the retract
statement is never dispatched through `ExecuteGroup`, and a Cypher-text
assertion that the unbound-row statement clears via `SET ... = null` and
contains no `REMOVE`. All were run RED then GREEN against the fixed writer.

Cost shape: one additional bounded autocommit `Execute` call per write batch
(the retract), scoped by `KubernetesNamespace.uid` (already an indexed MERGE
identity) with a small relationship-type expansion (`TARGETS_ENVIRONMENT`
fan-out from one namespace is at most 1 in the correct steady state). No
`LIMIT` drain loop needed -- the delete-index is tiny and bounded by the
batch's row count, the same fixed-fan-out class as the cloud-correlation
retract fixes.

## Observability Evidence:

No-Observability-Change. The retract statement carries the same
`StatementMetadataPhaseKey` / `StatementMetadataEntityLabelKey` /
`StatementMetadataSummaryKey` metadata convention as every other statement
this writer emits, and errors route through the existing
`WrapRetryableNeo4jError` classification. No metric name, span, log field,
queue stage, worker knob, or status field changes.
