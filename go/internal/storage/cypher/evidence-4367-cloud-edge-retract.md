# Evidence: cloud-correlation edge retracts dispatched through ExecuteGroup under-apply on NornicDB v1.1.11 (#4367 retract-depth backfill)

## Probe table

Probed directly against a lean `timothyswt/nornicdb-cpu-bge:v1.1.11` container
over the HTTP `tx/commit` auto-commit endpoint (`http://localhost:17481/db/nornic/tx/commit`),
before writing the live regression test, to decide whether the untyped
relationship-expansion retract shape in
`retractSecurityGroupSGRuleEdgesCypher` needed a per-rel-type rework (per the
open question in `docs/public/reference/nornicdb-pitfalls.md`).

| Probe | Statement shape | Result |
| --- | --- | --- |
| Seed | `CREATE (:CloudResource {uid:"sg-probe-1"}), (:SecurityGroupRule {uid:"rule-probe-1"})` | 2 nodes created |
| Seed edge | `MATCH (sg...), (rule...) MERGE (sg)-[rel:ALLOWS_INGRESS]->(rule) SET rel.scope_id=..., rel.evidence_source=...` | 1 edge created |
| Count before | `MATCH (sg:CloudResource {uid:"sg-probe-1"})-[rel]->(rule:SecurityGroupRule {uid:"rule-probe-1"}) RETURN count(rel)` | `1` |
| Retract (exact production shape, single auto-commit statement) | `MATCH (sg:CloudResource)-[rel]->(rule:SecurityGroupRule) WHERE rel.scope_id IN [...] AND rel.evidence_source = ... DELETE rel` | applied |
| Count after | same as "count before" | `0` |

**Conclusion:** the untyped `[rel]` expansion in
`retractSecurityGroupSGRuleEdgesCypher` matches and deletes correctly when run
as a single auto-commit statement on v1.1.11. It did **not** need a per-rel-type
rework. The pitfalls-doc open question ("probe before trusting it") is
resolved: this shape is sound; only the *dispatch path* (see below) was
broken.

## Problem

Five reducer-owned canonical writers under `go/internal/storage/cypher/` —
`KubernetesCorrelationEdgeWriter` (RUNS_IMAGE), `S3LogsToEdgeWriter`
(LOGS_TO), `S3ExternalPrincipalGrantWriter` (GRANTS_ACCESS_TO),
`SecurityGroupReachabilityWriter` (ALLOWS_INGRESS / ALLOWS_EGRESS / TO), and
`IAMInstanceProfileRoleEdgeWriter` (HAS_ROLE) — each dispatched their retract
statement(s) through a shared `dispatch()` helper:

```go
func (w *Writer) dispatch(ctx context.Context, stmts []Statement) error {
	if ge, ok := w.executor.(GroupExecutor); ok {
		return WrapRetryableNeo4jError(ge.ExecuteGroup(ctx, stmts))
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
```

`dispatch` is a reasonable *write*-path helper (an atomic multi-statement
MATCH-MATCH-MERGE batch benefits from one transaction), but every one of
these five writers also routed its *retract* DELETE(s) through the same
helper. In production, `cmd/reducer`'s `reducerNeo4jExecutor.ExecuteGroup`
(`cmd/reducer/reducer_executor_adapters.go`) is wired unconditionally for
every graph backend including NornicDB — unlike the separate
`semanticEntityExecutorForGraphBackend` path, which deliberately hides
`GroupExecutor` for NornicDB unless `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true`.
So on the real production executor, every one of these five retracts went
through `ExecuteGroup`, i.e. a managed Bolt transaction.

On the pinned NornicDB v1.1.11, a DELETE dispatched through `ExecuteGroup`
under-applies — even a **single** statement in the group (the general form of
the pitfall documented for the code-call/SQL/rationale/inheritance retracts in
`docs/public/reference/nornicdb-pitfalls.md`, "Node-Label Disjunction" section
and its managed-transaction-DELETE refinement). The identical statement run as
an auto-commit transaction (`Execute`) deletes correctly. `CodeInterprocEvidenceWriter`
and `SecurityGroupReachabilityWriter`'s ledger-anchored
`RetractSecurityGroupReachabilityByUIDs` already had a `dispatchRetract`
helper (sequential `Execute`, never `ExecuteGroup`) for exactly this reason;
the other four writers' retracts, and `SecurityGroupReachabilityWriter`'s own
whole-scope `RetractSecurityGroupReachability`, had not been converted.

## Fix

Each of the five writers now routes its retract statement(s) through a
`dispatchRetract` helper (sequential `Execute`, never `ExecuteGroup`):

- `KubernetesCorrelationEdgeWriter.dispatchRetract` (new) —
  `RetractKubernetesCorrelationEdges` now calls it instead of `dispatch`.
- `S3LogsToEdgeWriter.dispatchRetract` (new) — `RetractS3LogsToEdges`.
- `S3ExternalPrincipalGrantWriter.dispatchRetract` (new) —
  `RetractS3ExternalPrincipalGrants`.
- `IAMInstanceProfileRoleEdgeWriter.dispatchRetract` (new) —
  `RetractIAMInstanceProfileRoleEdges`.
- `SecurityGroupReachabilityWriter.RetractSecurityGroupReachability` now
  reuses the `dispatchRetract` helper that already existed on the same type
  for the ledger-anchored `RetractSecurityGroupReachabilityByUIDs` path
  (`security_group_reachability_edge_writer_ledger.go`), instead of
  duplicating it.

The write paths (`Write*`) are unchanged: they still dispatch through
`dispatch`, so a `GroupExecutor`-capable executor still batches multi-row
MATCH-MATCH-MERGE writes atomically. Only the retract paths were re-routed.

Each fixed writer carries a no-group unit guard built on
`sqlSequentialRecordingExecutor` (defined in
`edge_writer_sql_retract_test.go`) — a double that implements `GroupExecutor`
and records both `Execute` and `ExecuteGroup` calls — asserting zero
`ExecuteGroup` calls plus the expected sequential retract statement shape. A
plain `recordingExecutor` cannot detect a revert to the grouped dispatch:
without `GroupExecutor`, the old `dispatch()` takes its sequential fallback
and produces identical `Execute` calls. The guards, each verified to fail when
its writer's retract is reverted to `dispatch()`:

- `TestKubernetesCorrelationEdgeWriterRetractNeverGroups`,
  `TestS3LogsToEdgeWriterRetractNeverGroups`,
  `TestS3ExternalPrincipalGrantWriterRetractNeverGroups`, and
  `TestIAMInstanceProfileRoleEdgeWriterRetractNeverGroups`
  (`cloud_edge_retract_dispatch_test.go`);
- `TestSecurityGroupReachabilityRetractScopesByEvidenceAndScope` and
  `TestSecurityGroupReachabilityRetractEmptyScopesIsNoOp`
  (`security_group_reachability_edge_writer_test.go`), converted to the same
  double with a `len(groupCalls) == 0` assertion.

They mirror the SQL retract's
`TestEdgeWriterRetractEdgesSQLRelationshipRunsPerLabelStatementsSequentially`.

## Benchmark Evidence:

Failing-then-green live regression on the pinned production backend (behavior
change — the old dispatch path silently left stale edges after retract):

```bash
cd go && ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_DATABASE=nornic \
NEO4J_URI=bolt://localhost:17695 NEO4J_USERNAME=neo4j NEO4J_PASSWORD=change-me \
go test ./internal/replay/offlinetier/ -run 'TestReducerCloudEdgeRetractGraphTruth|TestReducerSecurityGroupReachabilityEdgeRetractGraphTruth' -count=1 -v
```

- RED (retract statements temporarily reverted to `w.dispatch` to reproduce
  the pre-fix executor shape):
  - `delta_tier_reducer_cloud_edge_retract_live_test.go:212: retract: in-scope RUNS_IMAGE gone: count = 1, want 0`
  - `delta_tier_reducer_security_group_reachability_retract_live_test.go:162: retract: in-scope ALLOWS_INGRESS gone: count = 1, want 0`

  These two verbatim lines cover RUNS_IMAGE and ALLOWS_INGRESS because each
  test fails fast at its first post-retract assertion; the remaining writers'
  pre-fix under-application follows from the same `dispatch()`-through-
  `ExecuteGroup` mechanism (identical helper, identical revert), and GREEN 3/3
  below covers all seven claimed types.
- GREEN: `ok  github.com/eshu-hq/eshu/go/internal/replay/offlinetier` 3/3 runs
  (~1.1s package wall each) after restoring `dispatchRetract`. The cloud-edge
  test writes and retracts RUNS_IMAGE, LOGS_TO, GRANTS_ACCESS_TO, and
  HAS_ROLE; the security-group reachability test writes and retracts
  ALLOWS_INGRESS, ALLOWS_EGRESS, and TO (against both a CidrBlock and a
  CloudResource endpoint label). Out-of-scope controls (a different
  `scope_id`, same `evidence_source`) survive every retract, and every
  endpoint/rule node survives (the `ExternalPrincipal` node in particular
  survives by design — it is a global identity the grant retract never
  DETACH-deletes, unlike the bounded `EvidenceArtifact` nodes other retracts
  own).

Cost shape: retract statements now run as 1-2 bounded auto-commit
transactions per writer instead of one managed transaction — the same
fixed-fan-out class as the code-call, inheritance, SQL, rationale, and
repo-dependency retract fixes. The prior grouped path bought its single
transaction by silently not deleting the edge, so there is no correct faster
baseline to regress against.

## Coverage claim scope

Claimed in `specs/replay-coverage-manifest.v1.yaml`:
RUNS_IMAGE, LOGS_TO, GRANTS_ACCESS_TO, HAS_ROLE, ALLOWS_INGRESS,
ALLOWS_EGRESS, TO — all seven proven live+retract above. `retractable_edge_type`
coverage moves from 34/52 to 41/52 (`bash scripts/verify-replay-coverage-gate.sh`).

**USES and MANAGES are explicitly NOT claimed here.** They are owned by
different writers, not the five cloud writers this backfill covers:

- `USES` in production is the `USES_PROFILE` relationship type, written and
  retracted by `EC2UsesProfileEdgeWriter`
  (`go/internal/storage/cypher/ec2_uses_profile_edge_writer.go`) — an EC2
  instance to IAM instance-profile edge, unrelated to the RUNS_IMAGE/LOGS_TO/
  GRANTS_ACCESS_TO/reachability/HAS_ROLE family this backfill targets.
- `MANAGES` corresponds to the Atlantis project `n`-relationship edge type
  (`internal/graph/edgetype.Manages`), written and retracted by
  `canonical_atlantis_edges.go` (AtlantisProject -> Directory), an entirely
  separate materialization domain.

Neither belongs to the five writers audited for this backfill, so neither is
claimed by this change; a future retract-depth pass for those two would need
its own live proof against their own writers.

Issue #5152 tracks the nine sibling writers that still route retract DELETEs
through their grouped `dispatch()` helper and need the same
`dispatchRetract` conversion with live proof: `ec2_uses_profile`,
`cloud_resource`, `gcp_cloud_resource`, `azure_cloud_resource`,
`workload_cloud_relationship`, `ec2_block_device_kms_posture`,
`code_taint_evidence`, `ec2_internet_exposure`, and `secrets_iam_graph`.

## Observability Evidence:

No-Observability-Change for signal names. Every retract keeps the existing
`WrapRetryableNeo4jError` classification and statement metadata
(`StatementMetadataPhaseKey` / `StatementMetadataEntityLabelKey` /
`StatementMetadataSummaryKey`); only the transaction-dispatch mechanism
changed (`ExecuteGroup` -> sequential `Execute`). No metric name, span, log
field, queue stage, worker knob, or status field changes.
