# AWS Relationship Edge Materialization Design

Status: design accepted; PR #1 (canonical nodes) landed; PR #2 (edge projection)
implemented and the three §10 principal-review decisions are resolved (USES edge
deferred, unresolved targets counted-only in v1, no cross-boundary fabrication).
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
| **PR #2** | `aws_relationship` -> edge projection on top of PR #1 nodes, using the `MATCH-MATCH-MERGE` graceful-degradation pattern, all three join modes, unresolved-edge accounting. | Implemented. |

PR #2 is fully designed in §5–§8 so the reducer/principal review can sign off on
the contract before the second implementation lands. PR #1 deliberately does not
write edges, so it cannot regress edge correctness; it only adds the missing
node substrate the edge join requires.

PR #2 landed the `DomainAWSRelationshipMaterialization` reducer domain
(`go/internal/reducer/aws_relationship_materialization.go` + the bounded join in
`aws_relationship_join.go`), the backend-neutral edge writer
(`go/internal/storage/cypher/cloud_resource_edge_writer.go`), the projector
intent (`go/internal/projector/aws_relationship_materialization_intents.go`), and
the `eshu_dp_aws_relationship_edges_total` counter. It gates on the PR #1
`GraphProjectionPhaseCanonicalNodesCommitted` phase on the CloudResource
keyspace, so edges never resolve against uncommitted nodes. Still requires
principal-engineer review before merge because it is reducer/graph-write work.

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
MERGE (source)-[rel:AWS_<RELATIONSHIP_TYPE>]->(target)
SET rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id
```

Two `MATCH`es before the `MERGE` mean: **if either endpoint node is absent, the
row produces no edge and no node is created.** This is exactly the
"forward-looking target degrades gracefully" requirement — a relationship to a
not-yet-scanned target type simply does not materialize, with zero crash and
zero fabricated node. This is the identical safety property the label-scoped SQL
relationship writer relies on (`buildLabelScopedSQLRelationshipCypher`).

The Cypher relationship type is a sanitized static token derived from the
observed AWS `relationship_type` value, for example
`ec2_subnet_in_vpc` -> `AWS_ec2_subnet_in_vpc`. The original
`relationship_type` remains a relationship property for readback. This keeps the
logical identity `(source, relationship_type, target)` without placing
`relationship_type` inside the relationship `MERGE` map, which NornicDB does not
route through its fast relationship upsert path. The token preserves the raw
relationship type's case so graph identity matches the reducer's documented
case-sensitive key. Unsafe relationship type text fails closed before Cypher is
built.

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

Conflict domain: `CloudResource` nodes keyed by `uid`, and AWS relationship
edges keyed by `(source_uid, relationship_type, target_uid)` through the static
`AWS_<raw relationship_type>` Cypher relationship type, both partitioned by scope
generation.

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
  follows the existing `RetractEdges` pattern keyed by `evidence_source`, and
  filters scopes on the **edge's own** `rel.scope_id` (set at write time from the
  intent), never on a node property: `CloudResource` nodes are cross-scope
  canonical and carry no `scope_id`, so a node-scoped predicate would match
  nothing and leak stale edges across generations.
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
  plus O(E) batched `MATCH-MATCH-MERGE` rows grouped by AWS relationship type.
  The index build is O(R). **No per-edge graph round trip; no unbounded
  variable-length traversal; no reducer serialization.**
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
`node_count`, and per-stage durations (load / extract / graph-write /
phase-publish) so an operator can separate fact-load time, graph-write time, and
readiness-gate publish time at 3 AM. PR #2 adds the `materialized` vs
`unresolved` edge counter.

After the node write succeeds (including a legitimate no-op for an empty
generation), the handler publishes the
`GraphProjectionKeyspaceCloudResourceUID` /
`GraphProjectionPhaseCanonicalNodesCommitted` readiness phase via the wired
`GraphProjectionPhasePublisher` (matching the AWS runtime contract checkpoint
defined by the shared reducer graph-projection phase constants). Stage B (PR #2) gates its edge projection
on this phase. The phase is **not** published when the node write fails, so the
edge build can never resolve against nodes that did not commit; it **is**
published for an empty generation so Stage B is not blocked forever on a scan
that found zero materializable resources.

No-Regression Evidence: the readiness publish reuses the existing
`publishIntentGraphPhase` helper (the same path `CloudAssetResolutionHandler`,
`WorkloadIdentityHandler`, and the semantic/platform materializers already use)
and emits one bounded `GraphProjectionPhaseState` row per intent. It adds no
graph round trip and no per-resource work, so the measured node-write and
extract benchmarks above are unchanged.

## 10. Principal-Review Decisions (Resolved)

These three questions were the locked principal-review gate for PR #2. All three
are resolved as below and the implementation honors each; they remain documented
so a future agent does not silently re-open them.

1. **Resolved — resource→resource edges only; `USES` deferred.** Label choice
   `CloudResource` is **settled by the existing read contract**: the query layer
   already depends on `CloudResource` nodes that no writer produced —
   `internal/query/compare.go` runs
   `MATCH (i:WorkloadInstance)-[r:USES]->(c:CloudResource)`,
   `internal/query/impact_resource_investigation.go` traverses `n:CloudResource`,
   and `internal/query/entity_map_traversal.go` resolves the label. PR #1 makes
   those queries return real data instead of empty results. PR #2 writes **only**
   relationship-type-specific
   `(:CloudResource)-[:AWS_<raw relationship_type>]->(:CloudResource)` edges; it
   does **not** write the `(:WorkloadInstance)-[:USES]->(:CloudResource)` edge
   (deferred to the deployment-mapping owner). The edge writer Cypher
   (`go/internal/storage/cypher/cloud_resource_edge_writer.go`) anchors both
   endpoints on `CloudResource`, with no `WorkloadInstance`/`USES` anywhere.
2. **Resolved — unresolved targets are counted-only in v1.** Unresolved
   forward-looking edges do **not** persist a `pending_aws_relationship` reducer
   fact in PR #2 v1; they stay counted and logged
   (`tally.unresolved` / `tally.unresolvedSource` in `aws_relationship_join.go`,
   surfaced through the `eshu_dp_aws_relationship_edges_total` counter under the
   `unresolved` join mode and the `unresolved_target_by_type` completion-log
   field) until a service-completion reopen path exists. A later enhancement may
   add the durable pending fact.
3. **Resolved — no node fabrication across the trust boundary.** Cross-account /
   cross-region targets resolve **only** if that account+region resource was
   scanned in the same scope. The in-memory join index is built solely from
   in-scope `aws_resource` facts (each carrying its own `account_id`/`region` in
   the uid), and the edge writer uses `MATCH` (never `MERGE`) on both endpoints,
   so an out-of-scope target is counted unresolved rather than fabricated. Proven
   by `TestExtractAWSRelationshipEdgeRowsCrossAccountTargetStaysUnresolved`.

## 11. PR #2 Evidence

Benchmark Evidence: focused Eshu-owned benchmarks on Apple M4 Pro, Go test
bench, 5,000 CloudResource resources (10,000 resource facts) and 5,000
`aws_relationship` facts per scope generation (a realistic large single-region
scan). No backend round trip (no-op group executor) so the numbers isolate the
bounded join and batching cost:

- `BenchmarkExtractAWSRelationshipEdgeRows` — full O(R) index build plus O(E)
  in-memory target resolution producing 5,000 resolved edge rows: ~23.2 ms/op,
  44.2 MB/op, 521,652 allocs/op. Linear in the corpus, **no per-edge graph round
  trip and no N+1 Cypher** — the §8 performance contract on the resolution path.
- `BenchmarkBuildCloudResourceJoinIndex` — O(R) index build over 5,000 resources
  (10,000 facts): ~15.1 ms/op, 22.1 MB/op, 330,133 allocs/op.
- `BenchmarkCloudResourceEdgeWriter` (`go/internal/storage/cypher`) — 5,000
  resolved edge rows shaped into relationship-type-grouped
  `MATCH-MATCH-MERGE` statements at the default 500/UNWIND: `2.31 ms/op`,
  `3.89 MB/op`, `40,099 allocs/op` on darwin/arm64 Apple M3 Pro. The graph
  commit is one grouped transaction via `GroupExecutor`, so the write side is
  bounded by distinct relationship types times `ceil(E_type/batchSize)`, never
  one statement per edge.

Performance Evidence: a remote NornicDB-backed Compose probe against a
populated graph showed the previous property-map relationship `MERGE` timed out
at `20s` for 12 rows, while the static relationship-type `MERGE` completed in
`0-1ms` per 12-row relationship-type batch for the same endpoint and row shape.
The same probe retracted 24 temporary edges with the evidence-source/scope
delete in about `2.1s`. The probe used temporary CloudResource nodes and did
not include environment-specific identifiers in this document. A follow-up probe
also confirmed NornicDB accepts the case-preserving lower-case static token
shape (`AWS_uses_kms_key`) with a `0ms` single-row upsert.

Observability Evidence: the handler records `eshu_dp_aws_relationship_edges_total`
dimensioned by `relationship_type` (bounded by the scanner fleet's closed
target-type set, guarded by #804's `relguard`) and `join_mode`
(arn / bare_id / correlation_anchor / unresolved), wraps work in the
`reducer.aws_relationship_materialization` span, and emits an `aws relationship
materialization completed` structured log carrying `scope_id`, `generation_id`,
`resource_fact_count`, `relationship_fact_count`, `edge_count`, the
resolved/unresolved-by-mode and -by-type tallies, and per-stage durations
(load / extract / retract / graph-write / total). An operator can answer "which
AWS relationship target types are losing edges, and is it because the target
service was not scanned yet?" at 3 AM without a per-edge log line.

## 12. The `container_image` Target Type Resolves (Issue #5450)

`ecs_task_definition_uses_image` and `lambda_function_uses_image` are the two
"uses image" relationships whose `target_type` is `container_image`. Section 5
above documents why: `container_image` is not an `aws_resource` fact, so
`DomainAWSRelationshipMaterialization`'s `cloudResourceJoinIndex` (built solely
from `aws_resource` facts) can never resolve it — both relationships
permanently landed in the `unresolved` tally with zero edges. Issue #5450
resolves this per relationship, under the EXACT-ONLY rule
`docs/internal/design/5472-graph-projection-policy.md` establishes for the
sibling ci_cd_run_correlation/container_image_identity/package domains:

- **`lambda_function_uses_image` → PROJECT (exact digest).** The relationship's
  `resolved_image_uri` attribute (`awsv1.RelationshipLambdaFunctionUsesImageAttributes`,
  decoded from `Attributes["attributes"].resolved_image_uri`, never the raw
  envelope payload) is an exact `registry/repository@sha256:digest` reference —
  the same identity strength `container_image_identity`'s `exact_digest`
  outcome requires. `containerImageNodeUIDFromDigestRef`
  (`go/internal/reducer/aws_cloud_image_join.go`) computes the `:ContainerImage`
  node uid directly from that reference, matching the OCI registry canonical
  writer's own formula (`oci-descriptor://<registry>/<repository>@<digest>`,
  see `internal/projector.ociDescriptorUID`) — including its normalization:
  the OCI registry collector (`internal/collector/ociregistry/identity.go`
  `NormalizeRepositoryIdentity`/`normalizeDigest`) unconditionally lowercases
  the scanned registry, repository, and digest before computing the node's
  real `repository_id`/descriptor identity, so `containerImageNodeUIDFromDigestRef`
  lowercases the same three components rather than preserving
  `resolved_image_uri`'s reported case (the Lambda `GetFunction` API response
  never passes through that collector normalization, so it can report any
  case). Lowercasing here removes what would otherwise be a hidden
  ECR-only-registries-happen-to-be-lowercase dependency, and a new
  additive domain, `DomainAWSCloudImageMaterialization`
  (`go/internal/reducer/aws_cloud_image_materialization.go`), two-MATCH-MERGEs
  `(:CloudResource)-[:AWS_lambda_function_uses_image]->(:ContainerImage)`
  through `CloudResourceContainerImageEdgeWriter`
  (`go/internal/storage/cypher/cloud_resource_container_image_edge_writer.go`).
  An unscanned image (the OCI registry has not observed that digest) is a
  `MATCH` miss, not a fabricated node — the identical graceful-degradation
  contract §5.3/§6 already establish.
- **`ecs_task_definition_uses_image` → STAYS Postgres-only (tag-only).** A task
  DEFINITION's container image (`ecs/relationships.go`'s
  `taskDefinitionImageRelationships`) carries only a tag, never a digest — the
  scanner's `DescribeTaskDefinition` response has no running-digest field to
  read. Promoting a tag-only reference would require resolving it through
  `container_image_identity`'s derived `tag_resolved` outcome, which is
  explicitly NOT exact (the #5472 policy's decision table lists only
  `exact_digest` as promotable) and is mutable (a tag can point to a different
  digest at different times) — projecting it as if it were exact would
  fabricate a stale-tag-shaped false edge. `DomainAWSCloudImageMaterialization`
  recognizes the relationship type and always skips it
  (`awsCloudImageSkipTagOnlyPostgresOnly`), counted and logged, never silently
  dropped.

### 12a. `running_image_ref` / `running_image_digest` Node Props

Deliverable (a) of issue #5450 is separate from the edge above: it surfaces
the ECS running task's `containers[].image`/`containers[].image_digest` and
the Lambda function's `image_uri`/`resolved_image_uri` as `running_image_ref`/
`running_image_digest` properties directly on the `CloudResource` node,
decoded through the typed `awsv1` attribute seam
(`go/internal/reducer/aws_resource_running_image.go`,
`cloudResourceRunningImageFields`) and merged into the row
`cloudResourceNodeRow` builds. Making the property REACH the graph requires
TWO steps, not one: the reducer-side row map must carry the key, AND
`baseCloudResourceUpsertCypher`'s `SET` clause
(`go/internal/storage/cypher/cloud_resource_node_writer.go`) must name it —
Cypher only persists a property a `SET r.<key> = row.<key>` fragment names; a
row map key with no matching `SET` fragment is silently dropped by the
backend, never an error. `baseCloudResourceUpsertCypher` unconditionally sets
`r.running_image_ref = row.running_image_ref` and
`r.running_image_digest = row.running_image_digest` alongside every other
optional field (`workload_id`, `service_name`, ...) using the writer's
existing convention: a row map with no such key resolves to Cypher `null` for
that row, which `SET` interprets as "no value this generation" (matching
every other optional `CloudResource` property's already-established
semantics — this is a full re-projection each generation, so clearing a
property genuinely absent this generation is correct, not a data-loss bug).

`running_image_digest` is normalized to the BARE digest
(`"sha256:<hex>"`) for BOTH resource types: ECS's `containers[].image_digest`
is already bare (the ECS `DescribeTasks` API's own shape), while Lambda's
`resolved_image_uri` is a full `registry/repository@digest` reference —
`lambdaFunctionImageFields` parses the digest suffix out via the shared
`digestFromImageRef` helper before writing it, so a consumer never has to
branch on `resource_type` to know which shape `running_image_digest` carries.
`running_image_ref` stays the full reference (tag-qualified, as configured)
for both resource types; the target-uid computation for the
`AWS_lambda_function_uses_image` edge above independently parses the digest
straight out of the relationship fact's own `resolved_image_uri` attribute
(not this node property), so the two paths do not share state.

This is modeled as an ADDITIVE SIBLING domain (its own `truth.Contract`,
`evidence_source = reducer/aws-cloud-image`), not an extension of
`DomainAWSRelationshipMaterialization`, because its target label
(`:ContainerImage`) and resolution strategy (a value on the relationship fact
itself, not the `aws_resource` join index) are categorically different from
every other AWS relationship type this domain resolves — mirroring the split
between `DomainKubernetesWorkloadMaterialization` and
`DomainKubernetesCorrelationMaterialization`, the closest existing precedent
for a cross-label workload/resource → image edge.

Prove-Theory-First Evidence: the two-MATCH-MERGE shape is architecturally
identical to the one §11 already measured (both `CloudResource` and
`ContainerImage` carry a uid uniqueness constraint / NornicDB uid lookup index,
`internal/graph/schema_tables.go` `uidConstraintLabels`), so the theory —
"a two-MATCH-MERGE with a static single-member relationship-type token against
two uid-constrained labels performs the same as the already-measured
`0-1ms`/batch CAN_ASSUME/AWS_&lt;type&gt; shape" — needed no fresh live probe.
The NEW-path cost is the Eshu-owned extraction and batching work, measured at
the same 5,000-row corpus scale as the sibling benchmarks (Apple M4 Pro, Go
test bench, no backend round trip):

- `BenchmarkExtractAWSCloudImageEdgeRows` — 5,000 `lambda_function_uses_image`
  relationships resolved against a 5,000-resource join index: `~16.8 ms/op`,
  `43.4 MB/op`, `346,599 allocs/op` — cheaper than the sibling
  `ExtractAWSRelationshipEdgeRows` (`~24.5 ms/op` at the same corpus scale,
  §11) because this domain builds no target-side lookup map; the target uid is
  computed, not joined.
- `BenchmarkCloudResourceContainerImageEdgeWriter`
  (`go/internal/storage/cypher`) — 5,000 resolved edge rows batched at the
  default 500/`UNWIND`: `~1.72 ms/op`, `3.61 MB/op`, `35,071 allocs/op` — in the
  same order of magnitude as the sibling `BenchmarkCloudResourceEdgeWriter`
  (`~1.6 ms/op`, `40,099 allocs/op`), and fewer allocations (a single fixed
  relationship-type token needs no per-type grouping).

No-Regression Evidence: `DomainAWSRelationshipMaterialization` itself is
UNCHANGED by this work — it still resolves only `CloudResource -> CloudResource`
and still counts `container_image`-targeted relationships as `unresolved` in
its own tally (the new domain is additive, reading the same fact kinds through
a second, independent handler). Verified with a live single-container NornicDB
retract proof (`go/internal/replay/offlinetier/delta_tier_reducer_cloud_image_edge_retract_live_test.go`,
`scripts/verify-replay-tier.sh`).

Observability Evidence: the handler records `eshu_dp_aws_cloud_image_edges_total`
dimensioned by `resolution_mode` (`container_image_digest`, the only mode this
domain resolves), wraps work in the `reducer.aws_cloud_image_materialization`
span, and emits an `aws cloud image materialization completed` structured log
carrying `scope_id`, `generation_id`, `resource_fact_count`,
`relationship_fact_count`, `edge_count`, the skip-reason tally
(`tag_only_postgres_only_policy` / `no_resolved_digest` / `unparseable_digest_ref`
/ `source_unresolved`), and per-stage durations. An operator can answer "is the
Lambda running-image edge landing, and did a generation skip every relationship
for the tag-only ECS policy reason (expected) versus an unresolved digest
(investigate)?" at 3 AM.
