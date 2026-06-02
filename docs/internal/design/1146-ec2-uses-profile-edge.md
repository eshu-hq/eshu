# EC2 USES_PROFILE Instance-Profile Edge Materialization Design — PR-B of the #1146 blast-radius arc

Status: design accepted for the USES_PROFILE slice (this PR-B). The profile → role
`HAS_ROLE` edge that completes the EC2 → profile → role → `CAN_ESCALATE_TO`
(#1134 PR3) chain remains the deferred follow-up — see §8. NEEDS PRINCIPAL REVIEW
— gated graph-write (`risk:schema`). The **dual-key readiness gate** below is the
principal-review focus.

Issue: #1146 (aws/deep: EC2 instance blast-radius). PR-A (merged, #1236, commit
`c6ce36e0`) materializes EC2 instances as first-class `:CloudResource` nodes on
the `cloud_resource_uid` keyspace and publishes the EC2 node readiness phase under
a distinct entity key. This PR-B projects only the first edge:
`(:CloudResource {ec2 instance})-[:USES_PROFILE]->(:CloudResource {iam instance profile})`.

Owners: AWS scanner fleet + reducer/projection owners.

This note is the durable design for projecting the `ec2_instance_posture`
`instance_profile_arn` field into canonical USES_PROFILE graph edges. It mirrors
the shipped #1144 PR2 `LOGS_TO` and #1134 PR2 `CAN_ASSUME` edge materializers
(edge-only on the existing `cloud_resource_uid` keyspace, closed single-member
relationship vocabulary, in-memory ARN join index, readiness-gated, skip+count for
unresolved targets). The one genuinely new thing here is the **dual-key readiness
gate**: unlike LOGS_TO/CAN_ASSUME, the two endpoints of a USES_PROFILE edge are
published under DIFFERENT entity keys, so the edge gates on TWO node phases.

## 1. Problem And Current State

PR-A materializes each running EC2 instance as a `:CloudResource` node keyed
`cloudResourceUID(account_id, region, "aws_ec2_instance", instance_id)` (bare
instance id, ARN fallback when blank). The `ec2_instance_posture` fact carries
`instance_profile_arn` — the ARN of the IAM instance profile attached to the
instance (empty when the instance has no profile). The IAM scanner already emits
each instance profile as an `aws_resource` fact
(`resource_type = aws_iam_instance_profile`, `arn`/`resource_id` =
`arn:aws:iam::<acct>:instance-profile/<name>`), which #805
`DomainAWSResourceMaterialization` materializes as a `:CloudResource` node keyed
`cloudResourceUID(account_id, region, "aws_iam_instance_profile", arn)`.

Today nothing turns `instance_profile_arn` into a queryable graph edge. The
blast-radius question "which instances run with which instance profile" (the first
hop of EC2 → profile → role → escalation) has the evidence and both endpoints
already exist as nodes — only the edge is missing. This is **edge-only**: no new
node type, no new keyspace, no schema constraint (the `CloudResource.uid`
constraint already exists in `go/internal/graph/schema.go`).

## 2. Scope: USES_PROFILE Only

For each `ec2_instance_posture` fact whose source instance forms a valid EC2
instance uid and whose `instance_profile_arn` resolves to a scanned IAM
instance-profile `:CloudResource` node, MERGE:

```
(:CloudResource {uid: ec2_instance_uid})
  -[:USES_PROFILE {scope_id, generation_id, evidence_source}]->
(:CloudResource {uid: instance_profile_uid})
```

The source is derived the SAME way PR-A derives the EC2 instance node uid (reusing
the bare-instance-id-with-ARN-fallback rule and the
`cloudResourceUID(account, region, "aws_ec2_instance", resource_id)` scheme), so
the edge's source `MATCH` hits exactly the node PR-A committed. The target is the
profile named by `instance_profile_arn`.

### Target resolution: ARN equality, never a prefix

`instance_profile_arn` is a full ARN. The ARN is partition-bearing — the code
NEVER hardcodes `arn:aws:`; it matches by exact ARN string against an in-memory
join index built once per scope generation from the `aws_resource`
`aws_iam_instance_profile` facts. Each index entry keys the scanned profile node
by its ARN (and by `resource_id`, which equals the ARN for instance profiles). A
profile ARN absent from the index did not scan as a node in this scope and resolves
to no edge.

### Skip rules (no fabrication, no dangle)

- **Blank `instance_profile_arn`** → the instance has no attached profile. No edge,
  and this is NOT a skip-error: it is the normal no-profile state and is not
  counted.
- **Tombstoned instance** → a terminated instance no longer runs, so PR-A
  materialized no node for it. No edge; not counted (consistent with PR-A's node
  tombstone rule — there is no node to anchor an edge to).
- **Source unresolved** → the posture fact carried neither an instance id nor an
  arn, so it cannot form the source uid. No edge, counted `source_unresolved`.
- **Target unresolved** → `instance_profile_arn` named a profile that was not
  scanned as an `aws_iam_instance_profile` `:CloudResource` node in this scope
  generation (cross-account, out-of-scope). No edge, counted `target_unresolved`.
  This is the trust-boundary rule, never fabricated — the same rule #805/#1134/#1144
  apply.

## 3. Resolution: In-Memory ARN Join Index

Resolution reuses the bounded in-memory join model from #1134 (`CAN_ASSUME`):
build a `byARN` map from the scope generation's `aws_iam_instance_profile`
`aws_resource` facts once, then resolve each `instance_profile_arn` by O(1) ARN
lookup. No per-edge graph round trip, no N+1 Cypher. An ARN absent from the index
did not scan as a node, so it produces no edge — graceful degradation, counted
`target_unresolved`. Because each index entry is derived from an `aws_resource`
fact that carried its own `account_id`/`region`, a cross-account profile resolves
only if that account's profile was scanned in the same scope — the trust-boundary
rule, never fabricated. The index keys only on `resource_type =
aws_iam_instance_profile`. First-writer-wins on an ARN collision (duplicate scan).

## 4. Closed-Vocabulary Static-Token Edge

`USES_PROFILE` is a closed single-member relationship vocabulary. The cypher
writer interpolates the validated static token into the relationship-type position
(which cannot be parameterized) only after the character-class + allowlist screen,
exactly like the #1144 PR2 `LOGS_TO` and #1134 PR2 `CAN_ASSUME` writers. Two
anchored `MATCH (:CloudResource {uid})` clauses precede the `MERGE` so a missing
endpoint is a no-op (never a fabricated node) and the two independent MATCHes never
form a cartesian product.

Edge identity is `(source_uid, USES_PROFILE, target_uid)`; the `MERGE` is on that
identity ONLY. The relationship type stays a static token, NEVER a property-keyed
relationship MERGE — a property-keyed relationship MERGE timed out at 20s vs 0–1ms
for a static token on NornicDB (#805 §5.3). Mutable
`scope_id`/`generation_id`/`evidence_source` are `SET` separately. Idempotent under
retries, duplicate facts, and reprojection (the extractor dedupes by
`(source_uid, target_uid)` before the write, and the prior-generation retract is
evidence-scoped to `evidence_source = reducer/ec2-uses-profile`).

## 5. The Dual-Key Readiness Gate (principal-review focus)

This is the one place USES_PROFILE genuinely differs from LOGS_TO/CAN_ASSUME, and
it is load-bearing. A USES_PROFILE edge consumes TWO `:CloudResource` node families
that publish their `cloud_resource_uid` / `canonical_nodes_committed` phase under
DIFFERENT entity keys for the same scope generation:

- the **EC2 instance source node** → entity key
  `ec2_instance_node_materialization:<scope>` (PR-A,
  `DomainEC2InstanceNodeMaterialization`), and
- the **IAM instance-profile target node** → entity key
  `aws_resource_materialization:<scope>` (#805,
  `DomainAWSResourceMaterialization`).

PR-A deliberately chose a distinct entity key for the EC2 node phase precisely so
this edge could gate on instance-node readiness independently. If the edge gated on
only one phase — or, worse, if both node domains shared one phase row — the edge
could open as soon as one endpoint family committed and resolve an edge against a
not-yet-materialized endpoint: a silent missed edge (wrong graph truth).

### Why a single `payload->>'entity_key'` match is not enough

The shipped single-phase edges (LOGS_TO, CAN_ASSUME, AWS relationship, COVERS) set
the edge intent's own entity key EQUAL to the single node phase they gate on, and
the durable claim gate matches `acceptance_unit_id =
COALESCE(NULLIF(payload->>'entity_key',''), scope_id)`. The security-group
reachability edge (#1135) gates on THREE phases but all three publish under the SAME
entity key (the SG fact's key), so it can still reuse `payload->>'entity_key'` for
all three.

USES_PROFILE cannot: its two endpoint phases use DIFFERENT entity keys, and an
intent carries only one entity key. So the gate requires **two literal-prefix
entity keys derived from `scope_id`**, not the intent's own entity key:

- `acceptance_unit_id = 'ec2_instance_node_materialization:' || scope_id` on
  `cloud_resource_uid` / `canonical_nodes_committed`, AND
- `acceptance_unit_id = 'aws_resource_materialization:' || scope_id` on
  `cloud_resource_uid` / `canonical_nodes_committed`.

The edge intent therefore carries its OWN distinct entity key
(`ec2_uses_profile_materialization:<scope>`), which keeps the edge's
readiness/conflict identity independent of either node domain. The two-key
requirement is expressed cleanly within the existing gate framework: the framework
already supports multiple `EXISTS` clauses per domain (the #1135 three-phase gate),
so the only new shape is matching a fixed entity-key prefix instead of
`payload->>'entity_key'`. No framework change was needed.

### Where the gate lives

- **handler-side:** `firstMissingNodePhase` queries the readiness lookup once per
  endpoint phase using a `GraphProjectionPhaseKey` built with each fixed entity-key
  prefix; a miss returns a retryable `ec2UsesProfileNodesNotReadyError` so the
  durable queue re-runs the intent once both phases commit.
- **queue-side (the load-bearing fence):** the dual `EXISTS` clause is added to
  `claimReducerWorkQuery` (`reducer_queue_claim_query.go`),
  `reducer_queue_batch.go` (both the eligible predicate and the same-conflict-key
  tiebreak subquery), and `status_blockage.go` (each missing phase surfaces its own
  `readiness` blockage row, distinguished by entity-key prefix).

Proof: `reducer_queue_ec2_uses_profile_readiness_test.go` drives all four
combinations — neither phase, only the instance node phase, only the profile node
phase, both phases — and asserts the intent is unclaimable until BOTH phases exist,
then claimable. The handler test
`TestEC2UsesProfileMaterializationGatesUntilBothPhasesCommit` proves the same at
the handler layer with a key-aware readiness lookup.

**Concurrency posture.** The write is idempotent by edge identity, partitioned by
scope conflict key, and uses no serialization workaround. "Serialization Is Not A
Fix" holds: the `MERGE` converges under concurrent reprojection without reducing
workers, batch size, or writer concurrency. Two instances sharing one profile
produce two distinct edges (no merge, no cartesian); duplicate facts dedupe to one
edge in the extractor's `seen` set before the write.

## 6. Telemetry

- Counter `eshu_dp_ec2_uses_profile_edges_total`, label `resolution_mode` (`arn` —
  the only resolution path, exact instance-profile ARN equality). Bounded
  cardinality. Counts materialized edges only.
- Counter `eshu_dp_ec2_uses_profile_skipped_total`, label `skip_reason`
  (`source_unresolved` / `target_unresolved`). Bounded closed enum. Counts posture
  facts that named a profile but produced no edge because an endpoint was not
  scanned. Blank-profile facts (no attached profile) are NOT counted here — they
  are the normal no-edge state.
- Span `reducer.ec2_uses_profile_materialization` wraps fact-load, the dual-key
  readiness gate, ARN join-index build, resolution, retract, and the batched
  MATCH-MATCH-MERGE write.
- Completion log carries resource-fact count, posture-fact count, edge count, and
  the bounded skip tally by reason so an operator can answer "which instances are
  losing USES_PROFILE edges, and is it because the profile's account was not
  scanned?" at 3 AM without a per-edge log line.
- The readiness gate surfaces a `readiness` conflict-domain blockage row per
  missing endpoint phase in `status_blockage.go`, so an operator can see WHICH node
  family (instance vs profile) has not committed.

## 7. Performance Impact Declaration

- **Stage:** reducer shared projection, one intent per scope generation that has
  `ec2_instance_posture` facts with a non-blank `instance_profile_arn`.
- **Cardinality:** one candidate edge per posture fact with an attached profile.
  Join index is the scope's `aws_iam_instance_profile` `aws_resource` fact count.
  All in-memory and bounded; no per-edge graph round trip.
- **Hot path:** the batched `UNWIND $rows MATCH-MATCH-MERGE` edge write, anchored
  on the `CloudResource.uid` uniqueness constraint at both `MATCH` sites —
  identical shape to #805 / #1134 PR2 / #1144 PR2, which are within the performance
  contract. Relationship type is a static token (not a relationship-property
  `MERGE`, which timed out at 20s on NornicDB per #805 §5.3).
- **Proof ladder:** focused reducer extractor + handler tests, cypher writer
  static-token tests + a writer benchmark vs the shipped LOGS_TO writer baseline,
  postgres dual-key readiness-gate query test. No new query shape vs the shipped
  edge writers, so a no-regression argument on the same write shape stands in for a
  fresh full-corpus bench in this slice.
- **Stop threshold:** if a corpus run shows the USES_PROFILE edge write exceeding
  the #1144/#1134 edge write per-row time by more than ~10%, profile before merge.

Benchmark Evidence: `BenchmarkEC2UsesProfileEdgeWriter`
(`go/internal/storage/cypher/ec2_uses_profile_edge_writer_bench_test.go`) measures
the statement-construction and batching cost of the USES_PROFILE edge writer for a
realistic per-scope-generation edge count (5000 rows) against a no-op group
executor, isolating Eshu-owned write-path work (batched MATCH-MATCH-MERGE row
shaping) from graph round trips and proving the write side has no N+1. Query shape:
`UNWIND $rows AS row MATCH (source:CloudResource {uid: row.source_uid}) MATCH
(target:CloudResource {uid: row.target_uid}) MERGE
(source)-[rel:USES_PROFILE]->(target) SET ...`. Backend: backend-neutral Executor
seam (NornicDB + Neo4j share the shape); `CloudResource.uid` uniqueness constraint
present from #805 PR1. MERGE identity: `(source_uid, USES_PROFILE, target_uid)`
only. Input cardinality at each anchor: both `MATCH` sites hit the
`CloudResource.uid` constraint index; batch size is the shared `DefaultBatchSize`
(500).

Measured (darwin/arm64, Apple M-series, `go test ./internal/storage/cypher -run
XXX_none -bench 'BenchmarkEC2UsesProfileEdgeWriter|BenchmarkS3LogsToEdgeWriter'
-benchmem -benchtime=2s -count=3`, 5000 rows, no-op group executor):

```
BenchmarkEC2UsesProfileEdgeWriter-12    ~1.40 ms/op    1968325 B/op    25071 allocs/op
BenchmarkS3LogsToEdgeWriter-12          ~1.40 ms/op    1968238 B/op    25071 allocs/op
```

The USES_PROFILE writer's per-op time, allocation count (25071), and bytes are
byte-for-byte indistinguishable from the shipped LOGS_TO writer baseline on the
identical 5000-row / batch-500 / no-op-executor shape — same write path, no new
per-row cost class.

No-Regression Evidence: the USES_PROFILE write is byte-for-byte the same
MATCH-MATCH-MERGE batched static-token shape as the shipped `LOGS_TO` writer (the
only differences are the relationship token `USES_PROFILE` and the
`source_uid`/`target_uid` row keys; the SET property set is identical:
`resolution_mode`/`scope_id`/`generation_id`/`evidence_source`). The benchmark
above and the shared edge-write contract show no new per-row cost class, no new
query shape, and no new index/constraint.

Observability Evidence: `eshu_dp_ec2_uses_profile_edges_total{resolution_mode}` and
`eshu_dp_ec2_uses_profile_skipped_total{skip_reason}` counters, the
`reducer.ec2_uses_profile_materialization` span, and the "ec2 uses-profile
materialization completed" structured completion log (resource-fact count,
posture-fact count, edge count, skip-reason tally) let an operator see materialized
vs skipped edges and the reason class for skips. The dual-key readiness gate
surfaces a per-endpoint `readiness` conflict-domain blockage row in
`status_blockage.go` when either node phase has not committed, so an operator can
see which endpoint family (instance vs profile) is the one that has not landed.

## 8. Deferred Follow-Up (NOT in this PR)

The **profile → role `HAS_ROLE` edge**
`(:CloudResource {instance-profile})-[:HAS_ROLE]->(:CloudResource {role})` is
deferred to a separate slice. The `aws_iam_instance_profile` `aws_resource` fact
already carries the profile's `role_arns` attribute, so that slice resolves each
role ARN to a scanned IAM-role `:CloudResource` node and writes the gated edge,
mirroring this one. With USES_PROFILE (this PR-B) plus HAS_ROLE landed, the full
chain EC2 → instance-profile → role → `CAN_ESCALATE_TO` (#1134 PR3) becomes a
single traversable graph path — the blast-radius arc #1146 set out to draw. That
edge is the clean reviewable next slice; it is not attempted here because it gates
on a different target node family (IAM role) and deserves its own review.
