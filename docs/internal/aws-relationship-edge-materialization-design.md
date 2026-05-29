# AWS Relationship Edge Materialization Design

Status: design accepted, foundation landed (PR #1).
Issue: #805 (parent #51, aws/H expansion).
Owners: AWS scanner fleet + reducer/projection owners.

This note is the durable design for projecting AWS `aws_relationship` facts into
canonical graph edges. It is the source of truth for the staged delivery, the
join-mode resolution, the concurrency/idempotency model, and the performance
contract. It lives under `docs/internal/` because it is maintainer design, not
operator-facing reference; it is intentionally outside the strict mkdocs
`public` build.

## 1. Problem And Current State

Multiple aws/H reviews (WAFv2 #795, Cognito #799, SageMaker #801) found that the
relationship-target correctness work across the scanner fleet is **latent**:
scanners emit `aws_relationship` facts with join-ready target identities, but
nothing materializes them as queryable graph edges.

A deep read of the pipeline established two facts that shape the whole design:

1. **`aws_relationship` facts have no graph consumer.** The only AWS consumer is
   the `aws_cloud_runtime_drift` reducer domain, which writes drift *findings*
   (orphan/unmanaged) as durable Postgres reducer facts. It never writes graph
   nodes or edges. Confirmed: no AWS node label or edge route exists in
   `go/internal/storage/cypher/`.

2. **`aws_resource` facts have no graph node materialization either.** The only
   consumer of `aws_resource` in `go/internal/projector` is
   `buildAWSCloudRuntimeDriftReducerIntent`, which merely *triggers* the drift
   reducer when AWS resource facts appear. There is no `CloudResource` /
   `AwsResource` node label, uniqueness constraint, or uid index in
   `go/internal/graph/schema.go`. The query layer references a `CloudResource`
   label speculatively (`internal/query/impact_resource_investigation.go`,
   `repository_infrastructure.go`), but no writer ever creates it.

The literal #805 ask — "materialize relationship facts as edges **between the
canonical AWS resource nodes**" — therefore has an unstated hard prerequisite:
**the canonical AWS resource nodes must exist first.** An edge projection built
today would `MATCH` zero endpoints and produce zero edges (or, if written
naively with `MERGE` on endpoints, would *fabricate* nodes — explicitly
forbidden by the issue and the life motto).

## 2. Staged Delivery

To respect "a smaller correct PR beats a large risky one," the work is split so
each stage is independently correct, gated, and measured.

| Stage | Scope | Status |
| --- | --- | --- |
| **PR #1** | Canonical AWS resource node materialization (`aws_resource` -> `CloudResource` nodes), graph schema (label, uid constraint, NornicDB uid lookup index, lookup indexes for ARN/resource-id/type), telemetry, docs. The prerequisite. | This PR. |
| **PR #2** | `aws_relationship` -> edge projection on top of PR #1 nodes, using the `MATCH-MATCH-MERGE` graceful-degradation pattern, all three join modes, unresolved-edge accounting. | Designed here, tracked as a follow-up issue. |

PR #2 is fully designed in §5–§8 so the reducer/principal review can sign off on
the contract before the second implementation lands. PR #1 deliberately does not
write edges, so it cannot regress edge correctness; it only adds the missing
node substrate the edge join requires.

## 3. Pipeline Placement

```
collector (aws scanners)
  -> emit aws_resource + aws_relationship facts (Postgres facts)
  -> projector enqueues reducer intent for the scope/generation
  -> REDUCER:
       Stage A (PR #1): aws_resource  -> CloudResource graph nodes
       Stage B (PR #2): aws_relationship -> canonical edges between CloudResource
                         (+ existing IAM/S3/etc.) nodes
  -> query/MCP/API read the canonical AWS graph
```

Stage B is strictly ordered after Stage A within a scope generation: edges can
only be resolved against nodes that the same generation already committed. The
reducer queue already gives us this ordering primitive via the
`GraphProjectionPhase` readiness gate (see
`go/internal/reducer/graph_projection_phase.go` and
`sharedProjectionReadinessPhase`); PR #2's edge domain gates on the
"AWS canonical nodes committed" phase the node writer publishes in PR #1.

## 4. Canonical AWS Resource Node Model (PR #1)

### 4.1 Node identity

- Label: `CloudResource` (matches the label the query layer already anticipates).
- Identity key (the `MERGE` anchor): `uid`, a stable, collision-resistant hash of
  `(account_id, region, resource_type, resource_id)`. This mirrors the
  `aws_resource` fact's `StableFactKey` inputs (see
  `awscloud.NewResourceEnvelope`), so the same resource maps to the same node
  across generations and across the relationship join.
- Mutable properties: `arn`, `resource_id`, `resource_type`, `name`, `state`,
  `account_id`, `region`, `service_kind`, `correlation_anchors`, plus the
  standard provenance set (`source_fact_id`, `stable_fact_key`, `source_system`,
  `source_record_id`, `source_confidence`, `collector_kind`, `scope_id`,
  `generation_id`, `evidence_source`).
- Secondary lookup properties for the edge join (PR #2 anchors): `arn`,
  `resource_id`, and `correlation_anchors`.

### 4.2 Write shape (idempotent, batched, no fabrication of foreign nodes)

```cypher
UNWIND $rows AS row
MERGE (r:CloudResource {uid: row.uid})
SET r.arn = row.arn,
    r.resource_id = row.resource_id,
    r.resource_type = row.resource_type,
    r.name = row.name,
    r.state = row.state,
    r.account_id = row.account_id,
    r.region = row.region,
    r.service_kind = row.service_kind,
    r.correlation_anchors = row.correlation_anchors,
    r.source_fact_id = row.source_fact_id,
    r.stable_fact_key = row.stable_fact_key,
    r.source_system = row.source_system,
    r.source_record_id = row.source_record_id,
    r.source_confidence = row.source_confidence,
    r.collector_kind = row.collector_kind,
    r.scope_id = row.scope_id,
    r.generation_id = row.generation_id,
    r.evidence_source = 'reducer/aws-resources'
```

`MERGE` is on `uid` only (the true identity), `SET` carries the mutable map —
the write-checklist rule from `cypher-query-rigor`. Duplicate input rows
(retries, duplicate facts) converge on the same node. This is the exact
batched-`UNWIND`/`MERGE` shape the Terraform-state canonical writer already uses
(`go/internal/storage/cypher/tfstate_canonical_writer.go`), so it engages the
same proven NornicDB hot path and Neo4j planner shape.

### 4.3 Schema

Add to `go/internal/graph/schema.go`:

- `CREATE CONSTRAINT cloud_resource_uid_unique ... FOR (r:CloudResource) REQUIRE r.uid IS UNIQUE`
  (handled by adding `CloudResource` to `uidConstraintLabels`, which also emits
  the NornicDB `nornicdb_cloud_resource_uid_lookup` index that the schema-backed
  MERGE lookup path requires — without it, MERGE on `uid` falls back to a label
  scan, the O(n²) trap called out in the schema comments).
- Lookup indexes for the PR #2 edge join anchors:
  `CREATE INDEX cloud_resource_arn ... ON (r.arn)`,
  `CREATE INDEX cloud_resource_resource_id ... ON (r.resource_id)`,
  `CREATE INDEX cloud_resource_type ... ON (r.resource_type)`.

## 5. Edge Projection (PR #2) — Join-Mode Resolution

`aws_relationship` facts carry `source_resource_id`, `source_arn`,
`target_resource_id`, `target_arn`, `target_type`, `relationship_type`,
`attributes` (see `awscloud.RelationshipObservation` and the relationship
envelope payload). The projection resolves **both endpoints to a
`CloudResource.uid`** using an **in-memory index built once per scope
generation** — never per-edge Cypher lookups.

### 5.1 The bounded join index (no N+1)

The handler loads, for the scope generation:

1. all `aws_resource` facts (to build the resolution index), and
2. all `aws_relationship` facts (the edges to project).

Both loads use the existing bounded `ListFactsByKind` fast path
(`go/internal/storage/postgres/facts_filtered.go`), scoped to
`(scope_id, generation_id, fact_kind)`. From the resource facts it builds three
lookup maps, all keyed to the resource's `uid`:

- `byARN[arn] -> uid`
- `byResourceID[resource_id] -> uid` (covers bare-id like `vpc-…`, `subnet-…`)
- `byAnchor[correlation_anchor] -> uid` (covers name-only targets)

Resolution is then O(1) map lookups in Go. The graph write is a single batched
`UNWIND` of already-resolved `(source_uid, target_uid)` pairs. **There is no
per-edge MATCH-scan and no N+1 Cypher.** This is the same architecture the SQL
relationship materialization uses (`ExtractSQLRelationshipRows` builds an
entity-by-name index, then resolves in memory; see
`go/internal/reducer/sql_relationship_materialization.go`).

### 5.2 The three documented join modes

For each `aws_relationship` fact, the **source** always resolves by
`source_arn` then `source_resource_id` (the scanner sets `SourceResourceID` to
the ARN or the bare id consistently). The **target** resolves by mode, chosen
from the fact's own fields — not guessed:

1. **`resource_id`-equality (ARN).** When `target_arn` is set (IAM role, S3
   bucket, KMS key, MQ configuration), resolve `byARN[target_arn]`, falling back
   to `byResourceID[target_resource_id]` (the resource scanner sets `resource_id`
   to the ARN when no separate id exists — see `NewResourceEnvelope`). This is
   the common case.

2. **bare-id.** When the target is a VPC/subnet/security-group-style id
   (`target_resource_id` = `vpc-…`, `subnet-…`, `sg-…`, `igw-…`, and
   `target_arn` empty), resolve `byResourceID[target_resource_id]`. These ids
   are the resource scanners' `ResourceID`, so the index hits directly.

3. **`correlation_anchor`.** When AWS exposes only a name (SageMaker
   endpoint->endpoint-config, endpoint-config->model, MQ shared-configuration
   fallback, CloudFormation stack-by-name), `target_resource_id` is a name and
   `target_arn` is empty. Resolve `byAnchor[target_resource_id]`, then
   `byResourceID[target_resource_id]`. The resource scanners publish their
   `CorrelationAnchors` (name, id, ARN) precisely so name-only references join
   here.

Mode selection is data-driven: try ARN index (if `target_arn` present or
`target_resource_id` looks like an ARN), then bare-id index, then anchor index.
The first hit wins; ties cannot occur because all three maps key into the same
`uid` space.

### 5.3 Edge write shape (graceful degradation, no fabrication)

```cypher
UNWIND $rows AS row
MATCH (source:CloudResource {uid: row.source_uid})
MATCH (target:CloudResource {uid: row.target_uid})
MERGE (source)-[rel:AWS_RELATIONSHIP {relationship_type: row.relationship_type}]->(target)
SET rel.evidence_source = row.evidence_source,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id
```

Two `MATCH`es before the `MERGE` mean: **if either endpoint node is absent, the
row produces no edge and no node is created.** This is exactly the
"forward-looking target degrades gracefully" requirement — a relationship to a
not-yet-scanned target type simply does not materialize, with zero crash and
zero fabricated node. This is the identical safety property the label-scoped SQL
relationship writer relies on (`buildLabelScopedSQLRelationshipCypher`).

The relationship `MERGE` identity is `(source, relationship_type, target)` so
re-projection is idempotent and duplicate facts converge on one edge.

## 6. Unresolved Targets

Edges whose target `uid` cannot be resolved in-memory (forward-looking type, or
target genuinely not in this generation) are **not dropped silently**. They are:

- counted into a `materialized` vs `unresolved` tally per relationship_type and
  resolution mode, emitted as a metric and a structured completion log
  (operator can answer "which AWS relationship types are losing edges, and is it
  because the target service isn't scanned yet?");
- the unresolved set is the diagnostic surface. PR #2 records unresolved-by-type
  counts; a later enhancement may persist a `pending_aws_relationship` reducer
  fact so a post-scan reopen can retry resolution once the target service is
  scanned (the bootstrap Phase-3 reopen pattern from the agent guide). PR #2's
  v1 surface is the counted/logged tally — bounded, honest, and not a silent
  drop.

This matches `eshu-correlation-truth`: positive (resolved edge materializes),
negative (forward-looking target stays unmaterialized, not fabricated), and
ambiguous (name-only target with no anchor hit stays unresolved, not guessed).

## 7. Concurrency And Idempotency Model

Conflict domain: `CloudResource` nodes keyed by `uid`, and `AWS_RELATIONSHIP`
edges keyed by `(source_uid, relationship_type, target_uid)`, both partitioned
by scope generation.

- **Claim/lease:** PR #1 node materialization runs as a reducer domain handler,
  claimed per `(scope_id, generation_id)` intent through the existing durable
  reducer queue — the same leasing the drift and SQL domains use. PR #2 edges
  run as a shared-projection domain partitioned by source `uid` (the
  `PartitionKey`), so concurrent workers never touch the same source node's
  outgoing edges. This preserves useful concurrency (no serialization) while
  removing overlap by partitioning on the conflict key.
- **Transaction scope:** one batched `UNWIND` group per partition, committed
  atomically via the existing `GroupExecutor` path in `EdgeWriter`.
- **Retry scope:** retries re-run the same idempotent `MERGE` on `uid` /
  `(source,type,target)`. A retried batch cannot duplicate nodes or edges.
- **Ordering:** within a generation, Stage B gates on the Stage A "AWS canonical
  nodes committed" phase (readiness gate), so edges never race ahead of their
  endpoints.
- **Stale / superseded generations:** the reducer's existing
  generation-supersede check skips an intent whose generation is no longer the
  active one for the scope (`ResultStatusSuperseded`), so a slow worker cannot
  overwrite a newer generation's graph. Retraction of a prior generation's edges
  follows the existing `RetractEdges` pattern keyed by `evidence_source`.
- **Empty / first generation:** zero facts -> zero rows -> no-op write, no
  retract on first generation (the `PriorGenerationCheck` skip the SQL domain
  uses).

Serialization is explicitly **not** used as a correctness device anywhere: the
write is idempotent under concurrent execution and partitioned by conflict key.

## 8. Performance Contract

- **Stage A (nodes):** O(R) `MERGE`-on-uid rows, R = AWS resources in the
  generation, batched at the default 500/UNWIND, same shape as the proven
  tfstate writer. Cost is bounded by R and engages the schema-backed uid lookup
  (constraint on Neo4j, uid index on NornicDB) — no label scans.
- **Stage B (edges):** O(E) in-memory map resolutions, E = relationship facts,
  plus O(E) batched `MATCH-MATCH-MERGE` rows. The index build is O(R). **No
  per-edge graph round trip; no unbounded variable-length traversal; no
  reducer serialization.**
- **Stop threshold:** if Stage A or B exceeds the known-normal band for the
  fixture corpus by >10% or >60s, profile before merge (per
  `eshu-diagnostic-rigor`).

PR #1 measures node materialization against a fixed AWS fixture set with the
focused writer benchmark + a no-regression check on the same input shape, since
it is a new write path rather than a change to an existing one.

## 9. Observability

- PR #1 reuses the shared canonical-write group metrics
  (`eshu_dp_shared_edge_write_*`) and adds a structured completion log with
  `scope_id`, `generation_id`, node row count, and stage durations
  (load / build-rows / graph-write), mirroring the SQL materialization
  completion log so an operator can tell fact-load time from graph-write time at
  3 AM.
- PR #2 adds a `materialized` vs `unresolved` counter dimensioned by
  `relationship_type` and `resolution_mode`, plus the unresolved-by-type tally
  in the completion log.

## 9a. PR #1 Evidence

PR #1 adds a new write path (CloudResource node materialization). It does not
change an existing hot path, so the contract is a same-shape no-regression
baseline rather than a before/after on an existing query.

Benchmark Evidence: focused write-path benchmarks on the new path, no backend
round trip (no-op group executor), Apple M4 Pro, Go test bench, 5,000
resources per scope generation (a realistic large single-region scan):

- `BenchmarkCloudResourceNodeWriter` — 5,000 CloudResource rows batched into
  10 UNWIND statements of 500 rows: ~3.10 ms/op, 6.33 MB/op, 25,068 allocs/op.
  This is Eshu-owned row-shaping + batching cost; the graph commit is the
  executor's measured `eshu_dp_neo4j_query_duration_seconds` (operation=write)
  on top.
- `BenchmarkExtractCloudResourceNodeRows` — projecting 5,000 aws_resource fact
  envelopes into deduped, uid-sorted node rows: ~11.17 ms/op, 16.70 MB/op,
  235,061 allocs/op. O(R) in-memory, no per-resource graph round trip — the
  bounded-join property the design requires for the PR #2 edge build.

No-Regression Evidence: the write reuses the proven TerraformResource
batched-UNWIND/MERGE-on-uid shape (`tfstate_canonical_writer.go`) at the same
default 500-row batch size, so it engages the same NornicDB schema-backed uid
lookup (the `nornicdb_cloud_resource_uid_lookup` index this PR adds) and the same
Neo4j planner path; no existing reducer, query, or write path changes shape.

Observability Evidence: every CloudResource write statement and group flows
through the production `InstrumentedExecutor`, which records
`eshu_dp_neo4j_query_duration_seconds` (operation=write) and
`eshu_dp_neo4j_batch_size`. The handler adds an `aws resource materialization
completed` structured log carrying `scope_id`, `generation_id`, `fact_count`,
`node_count`, and per-stage durations (load / extract / graph-write) so an
operator can separate fact-load time from graph-write time at 3 AM. PR #2 adds
the `materialized` vs `unresolved` edge counter.

## 10. Open Questions For Principal Review

1. Label choice `CloudResource` is **settled by the existing read contract**: the
   query layer already depends on `CloudResource` nodes that no writer produced
   — `internal/query/compare.go` runs
   `MATCH (i:WorkloadInstance)-[r:USES]->(c:CloudResource)`,
   `internal/query/impact_resource_investigation.go` traverses `n:CloudResource`,
   and `internal/query/entity_map_traversal.go` resolves the label. PR #1 makes
   those queries return real data instead of empty results. Confirm only that
   PR #1 should not also retro-fit the `(:WorkloadInstance)-[:USES]->` edge
   (deferred to the deployment-mapping owner; PR #1 only creates the nodes).
2. Whether unresolved forward-looking edges should persist a
   `pending_aws_relationship` reducer fact in PR #2 v1, or stay counted-only
   until a service-completion reopen path exists.
3. Cross-account / cross-region edges: targets in a different account/region than
   the source. The `uid` includes account+region, so a cross-account ARN target
   resolves only if that account was also scanned in the same scope. Confirm this
   is the intended boundary (it is the safe default: no fabrication across trust
   boundaries).
