# RUNS_IMAGE Relationship-Filter Read Path (#5436)

The `RUNS_IMAGE` edge (`KubernetesWorkload`-[:RUNS_IMAGE]->`OciImageManifest`/
`OciImageIndex`/`OciImageDescriptor`) was graph-written by
`kubernetes_correlation_edge_writer.go` (#388) but had no declared query/MCP
read path. This change adds it to the existing `analyze_infra_relationships` /
`POST /api/v0/infra/relationships` relationship-filter surface: one entry in
`infraCanonicalEdgeTypes`, one entry in `infraRelationshipTypeAliases`
(`what_runs_image`), one enum value on the MCP tool and the matching OpenAPI
schema, and one additive NornicDB index on `KubernetesWorkload.id`.

No-Regression Evidence: this is not a new Cypher template or a new code path.
`infraRelationshipTypeClause` already renders the inline `:TYPE1|TYPE2`
relationship-type filter for every existing alias -- `what_deploys` resolves to
three edge types, `what_provisions` to two -- and the `OPTIONAL MATCH
(n)-[r:TYPES]->(target)` / reverse pattern in `getRelationships`
(`infra_relationship_filter.go`) is unchanged byte-for-byte. Adding a fourth
single-type alias exercises the identical clause-building function
(`infraRelationshipTypeClause`) and the identical anchored `MATCH (n) WHERE
n.id = $entity_id` read; it does not add a MATCH clause, a traversal hop, or a
new anchor. `TestInfraRelationshipsHonorsRelationshipTypeFilter` and
`TestWhatDeploysSurfacesRuntimeDeploymentSourceEdge` already proved this shape
is bounded and correct for the multi-type case; the new
`TestInfraRelationshipsWhatRunsImageSurfacesOutgoingEdge` and
`TestInfraRelationshipsWhatRunsImageSurfacesIncomingEdge` prove the same shape
for the single-type RUNS_IMAGE case in both directions.

The one genuinely new statement is the additive
`nornicdb_kubernetes_workload_id_lookup` index
(`FOR (w:KubernetesWorkload) ON (w.id)`), which mirrors seven identical existing
single-property id-lookup index entries in `nornicDBMergeLookupIndexes`
(Repository, Workload, WorkloadInstance, Platform, Endpoint, CloudAction,
EvidenceArtifact) verbatim in form. It brings KubernetesWorkload to parity with
those labels rather than introducing a new index shape to benchmark; the
existing entries were each added without a dedicated per-index benchmark
because CREATE INDEX is schema DDL applied once at bootstrap, not a per-request
hot path. `TestSchemaStatementsForBackendAddsNornicDBMergeLookupIndexes` proves
the statement is emitted; `TestSchemaApplicationsDeclareCompatibilityDecision`
proves the schema-fingerprint bump is additive and NornicDB-only (the Neo4j
fingerprint is unchanged).

Proof commands:

```bash
cd go && go test ./internal/query -run 'TestInfraRelationship|TestResolveInfraRelationshipTypes' -count=1 -v
cd go && go test ./internal/graph -run 'TestSchemaStatementsForBackendAddsNornicDBMergeLookupIndexes|TestSchemaApplicationsDeclareCompatibilityDecision' -count=1 -v
cd go && go test ./internal/mcp -run 'TestEcosystemToolsAreRegistered' -count=1
cd go && go test ./cmd/golden-corpus-gate -run TestGoldenSnapshotInfraRelationshipsRequiresRunsImageEdge -count=1 -v
```

No-Observability-Change: the handler emits the same
`telemetry.SpanQueryInfraRelationships` span and the same
`eshu.relationship_filter` attribute it always has; `what_runs_image` only
changes the attribute's string value to `RUNS_IMAGE`, the same way every other
alias already does. No new metric, span, log scope, or graph write path was
added.
