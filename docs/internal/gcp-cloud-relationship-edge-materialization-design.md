# GCP relationship edge materialization (#2348)

## Why

`gcp_cloud_relationship` facts (Cloud Asset Inventory `relatedAsset` edges) are
emitted by the collector but never materialized. AWS has
`DomainAWSRelationshipMaterialization`; GCP had no equivalent, so GCP resources
carried no relationship edges in the graph. This domain mirrors the AWS edge path
for GCP, gated on the GCP `CloudResource` node substrate from #2358.

## Model

- Domain: `DomainGCPRelationshipMaterialization` (`gcp_relationship_materialization`).
- Handler: `GCPRelationshipMaterializationHandler` in
  `go/internal/reducer/gcp_relationship_materialization.go`.
- Join: `ExtractGCPRelationshipEdgeRows` in `go/internal/reducer/gcp_relationship_join.go`.
- Writer: `GCPCloudResourceEdgeWriter` in
  `go/internal/storage/cypher/gcp_cloud_resource_edge_writer.go` — a `GCP_`-prefixed
  sibling of the AWS edge writer (`GCP_RELATIONSHIP` statement label, evidence
  source `reducer/gcp-relationships`).
- Projector trigger: `buildGCPRelationshipMaterializationReducerIntent`, anchored
  on the first `gcp_cloud_relationship` fact and keyed to the same
  `gcp_resource_materialization:<scope>` entity key as the node intent so the
  readiness gate resolves the exact published phase.

### Readiness gate

The handler gates on the `cloud_resource_uid` / `canonical_nodes_committed`
phase that `DomainGCPResourceMaterialization` publishes under the
`gcp_resource_materialization:<scope>` acceptance unit. A gate miss returns a
retryable error so the durable queue re-runs the intent once GCP nodes commit —
edges never resolve against a generation whose nodes have not committed.

### Endpoint resolution

GCP resource identity is the globally-unique CAI `full_resource_name`. The join
index maps `full_resource_name -> uid` (the same uid the node materialization
committed), so both relationship endpoints resolve by exact name — O(1) per edge,
no per-edge graph round trip, no fuzzy matching. An endpoint not scanned in the
same scope is unresolved (graceful skip + count), the trust-boundary rule.

### support_state contract

The collector classifies each relationship:

| support_state | edge behavior |
| --- | --- |
| `supported` | resolve both endpoints; materialize if both resolve |
| `partial` | target opaque/cross-project — treat as unresolved, no edge |
| `unsupported` | provenance only — no edge |

A relationship whose provider `relationship_type` is not a safe Cypher token
(`[A-Za-z0-9_]`) is skipped and counted (`invalid_type`), never failing the
batched write. The cypher writer also fails closed on an unsafe token as
defense-in-depth.

### Edge write

Edges are `(:CloudResource)-[:GCP_<TYPE>]->(:CloudResource)` with rel properties
`relationship_type` (verbatim), `target_type`, `support_state`,
`resolution_mode`, `scope_id`, `generation_id`, `evidence_source`. The write is
idempotent by `(source_uid, relationship_type, target_uid)`; a missing endpoint
is a no-op (two `MATCH`es precede the `MERGE`), never a fabricated node. The
prior-generation retract is scoped to `evidence_source = reducer/gcp-relationships`
and the edge's own `scope_id` (CloudResource nodes carry no scope_id), skipped on
the first generation.

## Invariants

- GCP keeps its provider-specific edge family (`GCP_<TYPE>`, distinct evidence
  source); no generic cloud collapse.
- No fabricated or dangling edges; unresolved/partial/unsupported are counted,
  not invented.
- Idempotent under retries and reprojection.

## Evidence

No-Regression Evidence: `go test ./internal/reducer -run
'GCPRelationship|ExtractGCPRelationship' -count=1`;
`go test ./internal/storage/cypher -run 'GCPCloudResourceEdgeWriter' -count=1`;
bench `BenchmarkExtractGCPRelationshipEdgeRows` = 30.7 ms/op for 5,000 edges over
10,000 resources vs AWS `BenchmarkExtractAWSRelationshipEdgeRows` = 25.9 ms/op on
the same host (same bounded O(R+E) join). The GCP edge writer mirrors the proven
AWS MATCH-MATCH-MERGE template; the only new hot-path Cypher is the
`GCP_`-prefixed sibling.

Observability Evidence: `go test ./internal/reducer -run
'TestGCPRelationshipMaterializationRecordsPrometheusSignals|TestGCPRelationshipMaterializationMetricCarriesRelationshipTypeAndJoinMode|TestGCPMaterialization(SkipsNoOpGraphWriteDurations|SignalsReachPrometheusExposition)'
-count=1` proves the Prometheus metrics
`eshu_dp_gcp_materialization_facts_total`,
`eshu_dp_gcp_materialization_graph_writes_total`, and
`eshu_dp_gcp_materialization_duration_seconds` are emitted with bounded
`domain`, `fact_kind`, `kind`, and `write_phase` labels through the same
`/metrics` handler mounted by Compose runtimes. No-op graph writes and skipped
first-generation retracts do not emit duration samples. The
`eshu_dp_gcp_relationship_edges_total` counter (dimensioned by bounded
`relationship_type` plus `join_mode`, with sentinels for missing or invalid
types), `reducer.gcp_relationship_materialization` span, and completion log
still carry scope/generation context, counts, by-mode tallies, unresolved
breakdowns, and per-stage durations.

## Follow-up

- #2347 — correlate GCP IAM policy observations into secrets-IAM read models.
- #2349 — resolve the secrets-IAM graph projection gate.
