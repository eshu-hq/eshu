# S3 LOGS_TO Server-Access-Log Edge Materialization Design

Status: design accepted for the LOGS_TO slice (this PR2). The
`GRANTS_ACCESS_TO :ExternalPrincipal` node-then-edge slice was implemented
separately by issue #1231; internet exposure moved to its own node-property
projection design in `docs/internal/design/1232-s3-internet-exposure.md`.
NEEDS PRINCIPAL REVIEW - gated graph-write (`risk:schema`).

Issue: #1144 (aws/deep: S3 bucket posture graph projection; parent #51). PR1
(merged) emits the `s3_bucket_posture` fact with the `logging_target_bucket`
field. This PR2 projects only the server-access-log delivery (`LOGS_TO`) slice.

Owners: AWS scanner fleet + reducer/projection owners.

This note is the durable design for projecting the derived `s3_bucket_posture`
`logging_target_bucket` field into canonical
`(:CloudResource {s3 bucket})-[:LOGS_TO]->(:CloudResource {s3 bucket})` graph
edges. It mirrors the shipped #1134 PR2 `CAN_ASSUME` trust-graph edge
materializer (the exact analog: edge-only on the existing `cloud_resource_uid`
keyspace, closed single-member relationship vocabulary, in-memory join index,
readiness-gated, skip+count for unresolved targets). The rigor is copied, not
reinvented.

## 1. Problem And Current State

PR1 emits `s3_bucket_posture` facts — metadata-only derived S3 bucket security
posture per bucket. The `logging_target_bucket` field captures the bucket NAME
(e.g. `orders-logs`) that S3 server-access logs are delivered to; an empty value
means access logging is disabled. The S3 scanner also emits each bucket as an
`aws_resource` fact (`resource_type = aws_s3_bucket`), which #805 PR1
(`DomainAWSResourceMaterialization`) materializes as a `:CloudResource` node
keyed `cloudResourceUID(account_id, region, "aws_s3_bucket", arn)` where the
synthesized ARN is `arn:aws:s3:::<name>` (partition-bearing, NO account/region).

Today nothing turns the logging-target field into a queryable log-delivery
graph. The security/operations question "which buckets deliver access logs to
which log bucket" has the evidence (`logging_target_bucket`) and both endpoints
already exist as nodes — only the edge is missing. This is **edge-only**: no new
node type, no new keyspace, no schema constraint (the `CloudResource` uid
constraint already exists in `go/internal/graph/schema.go`).

## 2. Scope: LOGS_TO Only

For each `s3_bucket_posture` fact whose source bucket resolves to a scanned S3
`:CloudResource` node and whose `logging_target_bucket` name resolves to a
scanned S3 `:CloudResource` node, MERGE:

```
(:CloudResource {uid: source_bucket_uid})
  -[:LOGS_TO {scope_id, generation_id, evidence_source}]->
(:CloudResource {uid: target_bucket_uid})
```

The source bucket is the fact's own bucket (`bucket_arn` / `bucket_name`); the
target is the bucket named by `logging_target_bucket`.

### Target resolution: bucket NAME, not ARN

`logging_target_bucket` is a bare bucket NAME. S3 bucket names are globally
unique and S3 ARNs are `arn:aws:s3:::<name>` (no account/region), so the target
is resolved by matching a scanned S3 `:CloudResource` node by **bucket name**
through an in-memory join index built once per scope generation from the
`aws_resource` S3 facts. The index keys each scanned S3 bucket node by its name,
derived from the node's `name` field, the tail of its `arn:aws:s3:::<name>` ARN,
and its `s3://<name>` correlation anchor.

### Skip rules (no fabrication, no dangle)

- **Empty/blank `logging_target_bucket`** → logging disabled. No edge, and this
  is NOT a skip-error: it is the normal "no logging configured" state and is not
  counted in the unresolved tally.
- **Source bucket unresolved** → the fact's own bucket did not scan as an S3
  node (should not normally happen, but defended). No edge, counted
  `source_unresolved`.
- **Target bucket unresolved** → `logging_target_bucket` named a bucket that was
  not scanned as an S3 `:CloudResource` node in this scope generation
  (cross-account log bucket, out-of-scope region, or a centralized logging
  account that was not scanned). No edge, counted `target_unresolved`. This is
  the trust-boundary rule, never fabricated — the same rule #805/#1134 apply.

### Self-target is a real edge (deliberate, documented decision)

A bucket logging to itself (`logging_target_bucket == bucket_name`) is a legal,
real S3 configuration — unlike IAM self-assume, which carries no trust truth. A
self-LOGS_TO edge represents a real distinct log-delivery configuration, so it
**is emitted**. This is the one intentional divergence from the CAN_ASSUME
self-loop skip rule, and it is tested.

## 3. Resolution: In-Memory Join Index

Resolution reuses the bounded in-memory join model from #1134/#805: build a
`byName` map from the scope generation's S3 `aws_resource` facts once, then
resolve each source bucket and each `logging_target_bucket` by O(1) name lookup.
No per-edge graph round trip, no N+1 Cypher. A name absent from the index did
not scan as a node, so it produces no edge — graceful degradation, counted
`target_unresolved` (or `source_unresolved`).

Because each index entry is derived from an `aws_resource` fact that carried its
own `account_id`/`region`, a cross-account log target resolves only if that
account's bucket was scanned in the same scope — the trust-boundary rule, never
fabricated.

The index keys only on `resource_type = aws_s3_bucket` resources, because both
endpoints of a LOGS_TO edge must be S3 buckets. First-writer-wins on a name
collision (S3 names are globally unique, so a collision means duplicate facts;
the first stable entry wins).

## 4. Closed-Vocabulary Static-Token Edge

`LOGS_TO` is a closed single-member relationship vocabulary. The cypher writer
interpolates the validated static token into the relationship-type position
(which cannot be parameterized) only after the character-class + allowlist
screen, exactly like the #1134 PR2 `CAN_ASSUME` writer and the #1135 PR2b
`ALLOWS_INGRESS/EGRESS` writers. Two `MATCH (:CloudResource {uid})` clauses
precede the `MERGE` so a missing endpoint is a no-op, never a fabricated node.

Edge identity is `(source_uid, LOGS_TO, target_uid)`; the `MERGE` is on that
identity ONLY. The relationship type stays a static token, NEVER a
property-keyed relationship MERGE — a property-keyed relationship MERGE timed out
at 20s vs 0–1ms for a static token on NornicDB (#805 §5.3). Mutable
`scope_id`/`generation_id`/`evidence_source` are `SET` separately. The
`s3_bucket_posture` fact does NOT carry the access-log prefix
(`logging_target_prefix` lives only on the raw S3 `aws_resource` attributes, not
on the derived posture fact), so this slice adds no discriminator edge property;
if a future slice promotes the prefix onto the posture fact it would be `SET` as
`rel.log_prefix`, never added to the MERGE map. Idempotent under retries,
duplicate facts, and reprojection.

## 5. Readiness Gate And Concurrency

Both endpoints are S3 `:CloudResource` nodes published under the existing
`cloud_resource_uid` / `canonical_nodes_committed` phase (#805 PR1). The new
`DomainS3LogsToMaterialization` gates on that exact phase, identically to
`DomainAWSRelationshipMaterialization`,
`DomainObservabilityCoverageMaterialization`, and
`DomainIAMCanAssumeMaterialization`:

- handler-side: `canonicalNodesReady` returns a retryable
  `s3LogsToNodesNotReadyError` when the phase is not published, so the durable
  queue re-runs the intent once nodes commit;
- queue-side: the domain is added to the existing `cloud_resource_uid` readiness
  clause in `claimReducerWorkQuery` (`reducer_queue_claim_query.go`),
  `reducer_queue_batch.go` (both the eligible predicate and the same-conflict-key
  tiebreak), and `status_blockage.go`.

Conflict domain / key: the intent is anchored to the same
`aws_resource_materialization:<scope>` entity key as the #805/#1134 edge intents,
so the readiness slice matches the published phase row, and the prior-generation
retract is evidence-scoped to `evidence_source = reducer/s3-logs-to`.

**Concurrency posture.** The write is idempotent by edge identity, partitioned
by scope conflict key, and uses no serialization workaround. "Serialization Is
Not A Fix" holds: the `MERGE` converges under concurrent reprojection without
reducing workers, batch size, or writer concurrency. Duplicate facts dedupe to
one edge in the extractor's `seen` set before the write.

## 6. Telemetry

- Counter `eshu_dp_s3_logs_to_edges_total`, label `resolution_mode` (`name` —
  the only resolution path, bucket-name equality). Bounded cardinality. Counts
  materialized edges only.
- Counter `eshu_dp_s3_logs_to_skipped_total`, label `skip_reason`
  (`source_unresolved` / `target_unresolved`). Bounded closed enum. Counts
  posture facts that named a log target but produced no edge because an endpoint
  was not scanned. Logging-disabled facts (blank target) are NOT counted here —
  they are the normal no-edge state.
- Span `reducer.s3_logs_to_materialization` wraps fact-load, join-index build,
  resolution, retract, and the batched MATCH-MATCH-MERGE write.
- Completion log carries posture-fact count, edge count, and the bounded skip
  tally by reason so an operator can answer "which buckets are losing LOGS_TO
  edges, and is it because the central log account was not scanned?" at 3 AM
  without a per-edge log line.

## 7. Performance Impact Declaration

- **Stage:** reducer shared projection, one intent per scope generation that has
  `s3_bucket_posture` facts with a non-blank `logging_target_bucket`.
- **Cardinality:** one candidate edge per posture fact with logging enabled.
  Join index is the scope's S3 `aws_resource` fact count. All in-memory and
  bounded; no per-edge graph round trip.
- **Hot path:** the batched `UNWIND $rows MATCH-MATCH-MERGE` edge write, anchored
  on the `CloudResource.uid` uniqueness constraint at both `MATCH` sites —
  identical shape to #805 / #391 PR3 / #1134 PR2 / #1135 PR2b, which are within
  the performance contract. Relationship type is a static token (not a
  relationship-property `MERGE`, which timed out at 20s on NornicDB per #805
  §5.3).
- **Proof ladder:** focused reducer extractor + handler tests, cypher writer
  static-token tests + a writer benchmark, postgres readiness-gate query tests.
  No new query shape vs. the shipped edge writers, so a no-regression argument on
  the same write shape stands in for a fresh full-corpus bench in this slice.
- **Stop threshold:** if a corpus run shows the S3 LOGS_TO edge write exceeding
  the #805/#1134 edge write per-row time by more than ~10%, profile before merge.

Benchmark Evidence: `BenchmarkS3LogsToEdgeWriter`
(`go/internal/storage/cypher/s3_logs_to_edge_writer_bench_test.go`) measures the
statement-construction and batching cost of the LOGS_TO edge writer for a
realistic per-scope-generation edge count (5000 rows) against a no-op group
executor, isolating Eshu-owned write-path work (batched MATCH-MATCH-MERGE row
shaping) from graph round trips and proving the write side has no N+1. Query
shape: `UNWIND $rows AS row MATCH (source:CloudResource {uid: row.source_uid})
MATCH (target:CloudResource {uid: row.target_uid}) MERGE
(source)-[rel:LOGS_TO]->(target) SET ...`. Backend: backend-neutral Executor
seam (NornicDB + Neo4j share the shape); `CloudResource.uid` uniqueness
constraint present from #805 PR1. MERGE identity: `(source_uid, LOGS_TO,
target_uid)` only. Input cardinality at each anchor: both `MATCH` sites hit the
`CloudResource.uid` constraint index; batch size is the shared `DefaultBatchSize`
(500). This reuses the exact closed-vocab static-token edge-write shape #1134 PR2
(`CAN_ASSUME`), #805 (`aws_relationship_edges`), #391 PR3 (`AWS_COVERS_<signal>`),
and #1135 PR2b (`ALLOWS_INGRESS/EGRESS`, `TO`) already measured within the
repo-scale edge-write contract, so this slice adds no new hot-path Cypher shape
and no per-edge graph lookup.

Measured (Apple M4 Pro, `go test ./internal/storage/cypher/ -run XXX_none -bench
'BenchmarkS3LogsToEdgeWriter|BenchmarkCloudResourceEdgeWriter' -benchmem`, 5000
rows, no-op group executor):

```
BenchmarkCloudResourceEdgeWriter-12    627    1856441 ns/op    3885828 B/op    40099 allocs/op
BenchmarkS3LogsToEdgeWriter-12         849    1380425 ns/op    1968251 B/op    25071 allocs/op
```

The LOGS_TO writer's per-op time (1.38ms/5000 rows) is below the shipped
edge-write baseline (1.86ms/5000 rows on the same shape with one more SET
property), confirming no regression on the batched MATCH-MATCH-MERGE write path.

No-Regression Evidence: the LOGS_TO write is byte-for-byte the same MATCH-MATCH-
MERGE batched static-token shape as the shipped `CAN_ASSUME` writer (the only
differences are the relationship token `LOGS_TO` and the `source_uid`/`target_uid`
row keys; the SET property set is identical:
`scope_id`/`generation_id`/`evidence_source`). The benchmark above and the shared
edge-write contract show no new per-row cost class.

Observability Evidence: `eshu_dp_s3_logs_to_edges_total{resolution_mode}` and
`eshu_dp_s3_logs_to_skipped_total{skip_reason}` counters, the
`reducer.s3_logs_to_materialization` span, and the "s3 logs-to materialization
completed" structured completion log (posture-fact count, edge count, skip-reason
tally) let an operator see materialized vs. skipped edges and the reason class
for skips. The readiness gate surfaces a `readiness` conflict-domain blockage row
in `status_blockage.go` when canonical nodes have not committed.

## 8. Related S3 Follow-Ups (NOT in this PR)

These items from #1144's PR2 design were deliberately out of scope here because
they needed machinery this edge-only slice did not build:

1. **`GRANTS_ACCESS_TO :ExternalPrincipal`** — implemented separately by issue
   #1231. That slice consumes metadata-only `s3_external_principal_grant` facts,
   creates bounded `:ExternalPrincipal` identities keyed by principal kind/value,
   and writes `(:CloudResource)-[:GRANTS_ACCESS_TO]->(:ExternalPrincipal)` only
   after the source S3 bucket resolves to an existing CloudResource.

2. **Internet exposure** — implemented separately by
   `DomainS3InternetExposureMaterialization` in issue #1232. It is a
   node-property projection on existing S3 `CloudResource` nodes, not an edge,
   and therefore has its own conservative exposed / not_exposed / unknown model
   in `docs/internal/design/1232-s3-internet-exposure.md`.

External-principal access did not resolve trivially on the edge-only keyspace,
so it was not attempted here. The LOGS_TO edge remains the clean, reviewable
first slice of #1144 PR2.
