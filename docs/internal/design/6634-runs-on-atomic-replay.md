# RUNS_ON atomic replay contract (#6634)

Status: implementation in review. This note does not claim merge-ready proof.

## Decision

Keep the existing batched RUNS_ON Cypher for this change. Recognize its two
known atomic writer groups by exact templates and row chunks so a commit-time
uniqueness or relationship-snapshot conflict can retry the *whole* transaction.
Do not make every `UNWIND ... DELETE` or conditional `SET` replayable, and do
not accept an unchecked `ReplaySafe` marker from a caller.

The source-tag NornicDB v1.3.2 Bolt run of
`TestBoltRunsOnWritersReplaceDuplicateLegacyUnkeyedIdentities` completed with
one keyed relationship after each writer replaced duplicate unkeyed edges.
That is evidence for the current single-pair success path on that source
build. A separate source-tag v1.3.2 Bolt run of
`TestRunsOnPairIsolationLive` passed both two-pair writer orders and repeated
delivery, preserving off-diagonal legacy and foreign keyed edges. Its forced
statement error rolled back the full real Bolt group for each writer; a
synthetic typed snapshot conflict then exercised the production retry adapter
and converged to one complete tuple per requested pair. This does not prove a
naturally occurring commit conflict, behavior of the published pinned image,
or scaled cost. The older documented managed-transaction
`UNWIND ... MATCH ... DELETE` no-op is not reproduced by these runs; it is not
a reason to rewrite a working query without a failing probe.

## Transaction and retry boundary

Both writers target one `(WorkloadInstance)-[:RUNS_ON
{identity_key: 'canonical'}]->(Platform)` identity. The old relationships have
no `identity_key`; a pair can hold more than one. The two paths are:

| Writer | Atomic group | Ownership rule |
| --- | --- | --- |
| `WorkloadMaterializer` | Legacy unkeyed cleanup, keyed `MERGE`, then conditional complete-tuple `SET`, all for the same `instance_id`/`platform_id` chunk. The Platform node upsert precedes this group. | Stamp workload provenance only if the keyed edge has no source or already has the workload source. Preserve a cross-repo tuple. |
| `EdgeWriter` for cross-repo RUNS_ON | Legacy unkeyed cleanup and keyed `MERGE`/complete-tuple `SET` for the same `repo_id`/`platform_id` chunks. Other domain routes and evidence-artifact MERGEs can share the containing group. | Cross-repo provenance overwrites the full tuple, so it wins in either writer order. |

`reducerCypherExecutor.ExecuteCypherGroup` currently maps the workload group to
`cypher.Statement` values marked `OperationCanonicalUpsert`; `EdgeWriter` also
builds its cleanup through the ordinary upsert route. Keep those operation
values unless a separate routing audit proves a change safe: the operation is
used by NornicDB phase routing and telemetry, not just by retry classification.

The managed graph transaction either commits every statement or rolls back.
The outer `RetryingExecutor.ExecuteGroup` examines a failed group only after
the driver returns. Its generic replay classifier accepts MERGE and a narrow
predicate-scoped retract. The current `UNWIND` cleanup fails that classifier;
the workload group's `UNWIND ... MATCH ... SET` fails it too. A query-only
cleanup rewrite therefore cannot close the workload retry gap. Driver-managed
transient retries are separate from the outer commit-conflict retry. An unknown
commit outcome must remain outside this new permission.

On a rolled-back attempt, retrying the same immutable rows converges when only
the new writers are active: cleanup removes unkeyed edges in the same bound
pair set; keyed MERGE finds or creates one canonical identity; the workload
`SET` refreshes its own tuple or leaves a cross-repo tuple intact. If the
cross-repo writer commits between attempts, workload retry preserves it. If
the workload writer commits first, cross-repo retry replaces the complete
tuple. The contract must not infer correctness from the absence of an error;
direct edge count and complete property-tuple read-back are required.

## Narrow classifier exception

The implementation adds no replay marker or new operation value.
`classifyRetryableGraphWriteGroupError` calls
`isCanonicalRunsOnReplaySafeGroup` only when the generic
`allStatementsAreReplaySafe` check rejects a failed group. The exception still
requires an existing recognized commit-time uniqueness or relationship-snapshot
conflict; it does not admit unknown commit outcomes.

The final `cypher.Statement` validator checks two shapes:

1. For the cross-repo route, each
   `batchCanonicalRunsOnLegacyIdentityCleanupCypher` chunk must have one later
   `batchCanonicalRunsOnUpsertCypher` chunk with the same rows, in chunk order.
   Every row must have nonempty `repo_id`, `platform_id`, and
   `evidence_source`. All cleanup chunks precede the first upsert chunk; other
   statements may share the group only when they are the exact current
   repo-dependency, typed repo-relationship, or evidence-artifact upsert
   templates with `OperationCanonicalUpsert`. Arbitrary MERGE-shaped Cypher is
   not admitted. Both RUNS_ON statements retain `OperationCanonicalUpsert`.
2. For the workload route, the whole group must be exactly the three templates
   `batchRuntimePlatformRunsOnLegacyIdentityCleanupCypher`,
   `batchRuntimePlatformRunsOnEdgeUpsertCypher`, and
   `batchRuntimePlatformRunsOnOwnedEdgePropertiesCypher`, in order. All three
   statements carry the same nonempty `instance_id`/`platform_id`/
   `evidence_source` row chunk, with no extra parameters, and retain
   `OperationCanonicalUpsert`. The final template's direct assignments and
   null/own-source guard preserve a cross-repo tuple.

`reducer.IsWorkloadRunsOnReplayGroup` checks the reducer-owned template strings
and rows; `storage/cypher` already imports `reducer`, so this creates no import
cycle. Changed templates, operations, missing chunks, reordered statements, or
mismatched rows fail closed. An unrelated statement is never admitted by a
RUNS_ON marker, because there is no marker. Do not broaden the generic parser
to accept arbitrary `SET`, add `MERGE` text merely to satisfy its lexical
check, or detach cleanup from its atomic replacement write.

A `MATCH ... WHERE [i.id, p.id] IN $pairs` cleanup would avoid `UNWIND`, but
its exact Bolt semantics and cost at the configured batch size have not been
proven on the target backend. Independent `i.id IN $instance_ids` and
`p.id IN $platform_ids` filters are not pair-exact: they delete off-diagonal
relationships. Neither is an approved drop-in replacement.

## Proof and rollout limits

Review proof must include a live Bolt matrix for both writers: two
instances and two platforms (and two repositories for the cross-repo route),
requested diagonal pairs, off-diagonal unkeyed decoys, duplicate legacy edges,
and a foreign keyed survivor. Assert exact edge counts, surviving pairs, and
the coherent `confidence`/`reason`/`evidence_source`/`source_tool` tuple after
each writer order and repeated replay. Include repeated identical input pairs
and conflicting same-pair rows; reject the latter or prove a deterministic
tuple winner before declaring replay safe. Inject a failed atomic statement and a
commit conflict through the real adapter/retry path; assert rollback before
retry and convergence after it. Mutate each validator input in tests to prove
that unrelated or widened groups remain terminal. Compare same-shape group
duration and statement counts before and after at representative batch size.

The decision assumes no old, unkeyed RUNS_ON writer remains active during the
cutover. An old process can recreate an unkeyed edge *after* the new cleanup
commits or overwrite provenance; no classifier exception can fence that version
overlap. Rollout must drain or fence old writers, or explicitly limit the
guarantee and schedule a post-cutover reconciliation. Repeated delivery and
concurrent new-writer overlap are part of the proof; a worker-count reduction
is not a fix.

Successful calls keep the existing Cypher and dispatch path. The new
classification runs only after a failed call; retries can add bounded backoff
on the affected conflict path.
Observe `eshu_dp_neo4j_deadlock_retries_total{write_phase,reason}`, the
`neo4j transient error, retrying` log, shared-edge group duration and
statement-count metrics, and reducer retry/dead-letter outcomes. The evidence
note accompanying code must report actual timings and failure counts.

No-Regression Evidence: on Apple M5 Max, the unchanged successful
`RetryingExecutor.ExecuteGroup` dispatch path measured 35.00, 48.34, and
71.25 ns/op (64 B/op, 1 alloc/op) with
`go test ./internal/storage/cypher -run '^$' -bench
'^BenchmarkRetryingExecutorExecuteGroupSuccess5767$' -benchmem -benchtime=100x
-count=3`. The new classifier runs only after a failed group, so there is no
additional success-path Cypher, transaction, or round trip. These are current
local measurements, not a same-host before/after graph-backend comparison;
live retry and pair-matrix proof remains required before a merge-ready claim.

Observability Evidence: successful calls retain the existing group-duration
and statement-count signals. Recognized retry attempts continue to increment
`eshu_dp_neo4j_deadlock_retries_total` with bounded `write_phase` and `reason`
labels and emit the existing structured retry warning. Terminal failures still
reach the reducer retry/dead-letter path; the injected-conflict tests verify
retry admission but do not claim live telemetry capture.
