# GCP CloudResource node materialization (#2358)

## Why

`gcp_cloud_resource` facts (Cloud Asset Inventory resources) flow end-to-end
into the Postgres-backed cloud-inventory read model via
`DomainCloudInventoryAdmission`, but they never become `CloudResource` **graph
nodes**. AWS, by contrast, has `DomainAWSResourceMaterialization`, which writes
canonical `CloudResource` graph nodes and publishes the
`cloud_resource_uid` / `canonical_nodes_committed` readiness phase that the AWS
relationship edge projection gates on.

The GCP relationship edge projection (#2348) must `MATCH` `CloudResource` nodes
by `uid`. With no GCP node substrate, GCP relationship edges have nothing to
resolve against. This domain builds that substrate, mirroring the AWS path.

## Model

- Domain: `DomainGCPResourceMaterialization` (`gcp_resource_materialization`).
- Handler: `GCPResourceMaterializationHandler` in
  `go/internal/reducer/gcp_resource_materialization.go`.
- Writer: reuses the provider-neutral `CloudResourceNodeWriter`
  (`canonicalCloudResourceUpsertCypher`) — no new Cypher.
- Projector trigger: `buildGCPResourceMaterializationReducerIntent` enqueues one
  scope-keyed intent when any `gcp_cloud_resource` fact is present, anchored on
  the first such fact for a stable reducer claim.

### Node identity

GCP resource identity is the globally-unique CAI `full_resource_name`. The node
`uid` is `cloudResourceUID(project_id, location, asset_type, full_resource_name)`
— the same stable-ID function the AWS path uses, so GCP and AWS share the
`CloudResource` label and `cloud_resource_uid` keyspace without colliding (the
hashed inputs never overlap between a GCP full resource name and an AWS
account/region/ARN identity).

Node row mapping:

| Node property | GCP source field |
| --- | --- |
| `uid` | `cloudResourceUID(project_id, location, asset_type, full_resource_name)` |
| `resource_id` | `full_resource_name` (the relationship-edge join key) |
| `resource_type` | `asset_type` |
| `name` | `display_name` |
| `state` | `state` |
| `account_id` | `project_id` |
| `region` | `location` |
| `service_kind` | `asset_type_family` |
| `arn` | empty (GCP has no ARN) |

A fact missing `full_resource_name` or `asset_type` carries no materializable
identity and is dropped — never fabricated. Duplicate facts (retries,
overlapping scans) converge on one node by `uid`.

### Readiness phase

After the node write succeeds — or is a legitimate no-op on an empty generation
— the handler publishes the `cloud_resource_uid` /
`canonical_nodes_committed` readiness phase under the
`gcp_resource_materialization:<scope>` acceptance unit (the intent entity key).
This is distinct from the AWS `aws_resource_materialization:<scope>` unit, so the
GCP relationship edge stage (#2348) gates on GCP node readiness independently.
Publishing on an empty generation is required so the edge stage is not blocked
forever; publishing only after a successful write prevents edges resolving
against nodes that never committed.

## Invariants

- GCP resources never promote to ownership truth; this is observed-resource node
  truth only.
- No generic cloud collapse: GCP keeps its provider-specific source contract and
  its own evidence source (`reducer/gcp-resources`), distinct from
  `reducer/aws-resources`.
- The node write is idempotent by `uid` and reuses the proven AWS writer Cypher.

## Evidence

No-Regression Evidence: `go test ./internal/reducer -run
'GCPResourceMaterialization|ExtractGCPCloudResourceNodeRows' -count=1`; bench
`BenchmarkExtractGCPCloudResourceNodeRows` measured 13.0 ms/op for 5,000
resources vs the AWS `BenchmarkExtractCloudResourceNodeRows` at 17.7 ms/op on the
same host. The graph write reuses the unchanged `canonicalCloudResourceUpsertCypher`
node writer, so no new hot-path Cypher is introduced.

Observability Evidence: `go test ./internal/reducer -run
'TestGCPResourceMaterializationRecordsPrometheusSignals|TestGCPMaterialization(SkipsNoOpGraphWriteDurations|SignalsReachPrometheusExposition)'
-count=1` proves the Prometheus metrics
`eshu_dp_gcp_materialization_facts_total`,
`eshu_dp_gcp_materialization_graph_writes_total`, and
`eshu_dp_gcp_materialization_duration_seconds` are emitted with bounded
`domain`, `fact_kind`, `kind`, and `write_phase` labels through the same
`/metrics` handler mounted by Compose runtimes. No-op graph writes do not emit a
`graph_write` duration sample. The `gcp resource materialization completed` log
still carries scope/generation context, counts, and per-stage durations for
exact-run diagnosis.

## Follow-up

- #2348 — materialize GCP relationships into graph edges, gated on this node
  phase, resolving endpoints by `full_resource_name`.
