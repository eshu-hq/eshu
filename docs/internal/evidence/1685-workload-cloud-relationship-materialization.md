# Workload Cloud Relationship Materialization Evidence (#1685)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Workload Cloud Relationship Materialization (#1685)

`DomainWorkloadCloudRelationshipMaterialization` promotes only exact
`aws_resource.workload_id` anchors with an explicit `environment` into canonical
`(:WorkloadInstance)-[:USES]->(:CloudResource)` graph truth. Service-name-only
anchors, config-read evidence, ambiguous multi-workload anchors, missing
environments, missing CloudResource identity, and tombstones remain candidate or
missing-evidence inputs for query surfaces rather than becoming ownership truth.
The handler is additive and registers only when a FactLoader and
`WorkloadCloudRelationshipEdgeWriter` are wired.

The queue gate waits for the CloudResource endpoint family before any
retract/write:

- `cloud_resource_uid/canonical_nodes_committed` for
  `aws_resource_materialization:<scope>`

Workload endpoints are protected by the Cypher writer's exact
`Workload {id}`/`WorkloadInstance.environment` `MATCH` path. Missing workload
materialization is therefore a no-op for that row, not a cross-scope queue block
and not fabricated graph truth.

No-Regression Evidence: `go test ./internal/reducer -run
'WorkloadCloudRelationship' -count=1` proves exact workload-anchor promotion,
environment-specific dedupe, service-name-only rejection, ambiguous-anchor
rejection, CloudResource readiness gating, stale edge retraction, and additive
handler wiring. `go test ./internal/storage/cypher -run
'WorkloadCloudRelationshipWriter' -count=1` proves the writer matches existing
`Workload`/`WorkloadInstance` and `CloudResource` endpoints before MERGEing
`USES`, and that scoped retract deletes only reducer-owned edges. `go test
./internal/query -run 'ServiceCloudResource|Compare' -count=1` proves
service-story reads only materialized `USES` edges as promoted cloud
dependencies, keeps unmaterialized candidates separate, and keeps compare
surfaces aligned with the reducer-owned relationship payload.

Observability Evidence: this slice adds no metric labels or new runtime knobs.
Operators diagnose it through the existing reducer execution span/counter,
retryable readiness errors, `fact_work_items` status/failure fields, graph
query-duration metrics tagged by statement summary
`edge=WORKLOAD_USES_CLOUD_RESOURCE`, and service-story query stage timing. The
queue gate keeps predictable endpoint-readiness waits pending instead of
inflating retry/failure counters.
