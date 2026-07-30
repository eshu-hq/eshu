# Service Deployment, Runtime, And Dependency Evidence Families (#1985, #1986, #1987)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Service deployment evidence family (#1985)

The deployment evidence family extends the Stage-1 service materialization
lineage (#1943) without a new table or a new reducer domain. On each
`service_catalog_correlation` intent, `ServiceCatalogCorrelationHandler` (when
both `MaterializationWriter` and `DeploymentRelationshipLoader` are wired) loads
the correlated services' repositories' active resolved deployment relationships
in one bounded `GetResolvedRelationshipsForRepos` call, then emits one
deployment-family `service_evidence_snapshots` row per relationship into the same
service generation as ownership. The row identity is
`ServiceDeploymentEvidenceKey(service_id, identity)` where `identity` is a sha1
digest of the relationship's generation-independent natural key
(`relationship_type`, `source_repo_id`, `target_repo_id`, `source_entity_id`,
`target_entity_id`). The resolver primary key `resolved_id` and the query-layer
`artifact_id` both embed the resolution generation id, so neither is usable as a
diff key; the natural-key digest is the identity-vs-generation split from design
#1231. Only the deployment relationship classes
(`DEPLOYS_FROM`/`DISCOVERS_CONFIG_IN`/`PROVISIONS_DEPENDENCY_FOR`/`RUNS_ON`) are
admitted; dependency edges are a separate follow-up family.

No-Regression Evidence: `go test ./internal/reducer -run
'ServiceDeployment|BuildServiceDeployment|ServiceMaterialization|ServiceCatalogHandler'
-count=1` proves deployment rows carry generation-stable keys (no embedded
`resolved_id`/generation), a changed payload hash flips the generation while an
identical re-materialization is a no-op, dropped relationships are tombstoned,
the deployment family is purely additive (no deployment loader leaves the
generation ownership-only and Stage-1 ownership tests stay green), and the
repo-scoped relationship load runs once per intent. `go test
./internal/storage/postgres -run TestComputeServiceChangedSinceDelta -count=1`
proves the family-generic delta SQL (grouped by `evidence_family`) classifies
deployment added/updated/unchanged/retired/superseded with bounded ordered
samples and reports the deployment family with zero deltas for an
ownership-only fixture, never silently dropping it. The deployment rows reuse the
existing `service_evidence_snapshots_diff_idx` (`generation_id`,
`evidence_family`, `service_evidence_key`) the Stage-1 delta query already
drives, so no new index or schema migration is added. Live-Postgres SQL is
proven by the same fake `Queryer`/`Rows` harness Stage-1 uses; the live SQL gate
is the Postgres integration suite in CI (`scripts/test-verify-package-docs.sh`
keeps the schema/read-model docs in lockstep, and the compose-backed
storage/postgres integration gate exercises the real `service_evidence_snapshots`
table).

Observability Evidence: this slice adds no new metric instrument, label, queue
domain, lease, or runtime knob. The deployment family lands inside the existing
`service_catalog_correlation` reducer execution span/counter and the same
service materialization commit path Stage-1 already instruments; operators
diagnose it through reducer run spans, execution counters, `fact_work_items`
status/failure fields, the durable `service_materialization_generations`/
`service_evidence_snapshots` rows, and the `get_service_changed_since`
API/MCP/CLI read surface that now reports the deployment family alongside
ownership.

## Service runtime evidence family (#1986)

The runtime evidence family extends the same service materialization lineage
(#1943) without a new table or a new reducer domain. On each
`service_catalog_correlation` intent, `ServiceCatalogCorrelationHandler` (when
both `MaterializationWriter` and `RuntimeInstanceLoader` are wired) loads the
correlated services' repositories' materialized runtime instances in one bounded
`GetRuntimeInstancesForRepos` call, then emits one runtime-family
`service_evidence_snapshots` row per instance into the same service generation as
ownership and deployment. The row identity is
`ServiceRuntimeEvidenceKey(service_id, instance)` =
`runtime:<service_id>:<platform_kind>:<environment>:<workload_ref>`, where
`workload_ref` is the durable `WorkloadInstance` id
(`workload-instance:<workload_name>:<environment>`). The reducer projection
(`projection.go`) constructs that instance id and the platform kind from durable
workload/environment identity, and the runtime read model
(`query.buildServiceDeploymentLanes` over `workloadContext["instances"]`,
`entity_workload_context.go`) reads `i.id`, `i.environment`, `p.kind`, `p.name`
off the graph nodes. None of these embed a resolution or materialization
generation id, so the runtime key is generation-stable — unlike the deployment
`resolved_id`, which digests the resolution generation. An instance without a
durable workload ref cannot be keyed and is dropped rather than keyed on an empty
identity.

No-Regression Evidence: `go test ./internal/reducer -run
'ServiceRuntime|BuildServiceRuntime|ServiceMaterialization|ServiceCatalogHandler'
-count=1` proves runtime rows carry generation-stable keys (no embedded
generation/`resolved_id`/`instance_id`), a changed payload hash flips the
generation while an identical re-materialization is a no-op, dropped instances
are tombstoned, the runtime family is purely additive (no runtime loader leaves
the generation without runtime rows and ownership/deployment tests stay green),
and the repo-scoped instance load runs once per intent. `go test
./internal/storage/postgres -run TestComputeServiceChangedSinceDelta -count=1`
proves the family-generic delta SQL (grouped by `evidence_family`) classifies
runtime added/updated/unchanged/retired/superseded with bounded ordered samples
and reports the runtime family with zero deltas for a non-runtime fixture, never
silently dropping it. The runtime rows reuse the existing
`service_evidence_snapshots_diff_idx` (`generation_id`, `evidence_family`,
`service_evidence_key`) the Stage-1 delta query already drives, so no new index
or schema migration is added. Live-Postgres SQL is proven by the same fake
`Queryer`/`Rows` harness Stage-1 uses; the live SQL gate is the Postgres
integration suite in CI.

Observability Evidence: this slice adds no new metric instrument, label, queue
domain, lease, or runtime knob. The runtime family lands inside the existing
`service_catalog_correlation` reducer execution span/counter and the same service
materialization commit path Stage-1 already instruments; operators diagnose it
through reducer run spans, execution counters, `fact_work_items` status/failure
fields, the durable `service_materialization_generations`/
`service_evidence_snapshots` rows, and the `get_service_changed_since`
API/MCP/CLI read surface that now reports the runtime family alongside ownership
and deployment.

### Production runtime instance loader (`GraphServiceRuntimeInstanceLoader`)

The `#1986` emitter above only runs when a `RuntimeInstanceLoader` is wired. The
production loader is `GraphServiceRuntimeInstanceLoader` (`cmd/reducer/main.go`
sets `DefaultHandlers.ServiceRuntimeInstanceLoader = reducer.
GraphServiceRuntimeInstanceLoader{Graph: graphReader}`). It is the graph-backed
analogue of the Postgres deployment loader: for each correlated service's
repository it issues one bounded read of the canonical graph and maps the
materialized `WorkloadInstance`/`Platform` nodes into `ServiceRuntimeInstance`
values. It reads only durable identity (`i.id`, `i.environment`, `i.name`,
`i.materialization_confidence`, `p.kind`, `p.name`) — never a `generation_id`,
`resolved_id`, or `materialization_generation` — so the runtime key stays
generation-stable.

Query shape and selectivity: the loader mirrors the query surface's
WorkloadInstance reads (`entity_workload_context.go`
`fetchWorkloadInstances`/`fetchWorkloadPlatformRows`) so loader truth and API
truth come from the same node reads, using two scalar queries per repository
rather than an OPTIONAL map projection. The query layer enforces this shape for
NornicDB optional-projection safety
(`TestFetchWorkloadContextUsesScalarQueriesForNornicDBOptionalProjectionSafety`),
so the loader follows it deliberately. First the WorkloadInstance read anchors on
the `workload_instance_repo_id` index (`graph/schema.go`:
`CREATE INDEX workload_instance_repo_id ... FOR (i:WorkloadInstance) ON
(i.repo_id)`):

```cypher
MATCH (i:WorkloadInstance {repo_id: $repo_id})
RETURN i.repo_id AS repo_id, i.id AS instance_id, i.name AS workload_name,
       i.environment AS environment,
       i.materialization_confidence AS materialization_confidence
ORDER BY instance_id
```

then the RUNS_ON platforms are read for the exact batch of instance ids, anchored
on the indexed WorkloadInstance ids (`workload_instance_id` /
`nornicdb_workload_instance_id_lookup`) so it is one bounded read rather than a
round trip per instance:

```cypher
MATCH (i:WorkloadInstance)-[:RUNS_ON]->(p:Platform)
WHERE i.id IN $instance_ids
RETURN i.id AS instance_id, p.kind AS platform_kind, p.name AS platform_name
ORDER BY instance_id, platform_kind
```

Both are indexed-anchor scalar reads (no OPTIONAL MATCH, no map projection). The
in-process join keeps an instance that has a durable environment but no inferred
platform (empty platform fields), and an instance with multiple inferred
platforms yields one runtime value per platform, which is correct because each
platform kind is a distinct runtime identity. Expected cardinality is bounded:
the input is the distinct set of correlated service repositories for one
`service_catalog_correlation` intent (small), each repository has a handful of
`WorkloadInstance` nodes, and the `RUNS_ON` fan-out per instance is small. Two
indexed reads run per repository (instances, then their platforms), so total
graph round trips are twice the distinct service-repo count for the intent.

Performance Evidence: this read previously did not execute at all — the loader
was unwired, so the runtime family emitted zero rows in production. The change
adds two bounded, indexed graph reads per correlated service repository (the
WorkloadInstance read then its RUNS_ON platform read), gated behind the
already-additive runtime family (only when `MaterializationWriter` is also
wired), on the `service_catalog_correlation` reduce path that already performs
the ownership and deployment reads. Backend: NornicDB (default
canonical) / Neo4j (compatibility); schema state: the
`workload_instance_repo_id` index from `graph/schema.go` (apply
`eshu-bootstrap-data-plane` before `eshu-bootstrap-index`). No-Regression
Evidence: `go test ./internal/reducer -run
'GraphServiceRuntimeInstanceLoader|ServiceRuntime|ServiceCatalogHandler' -count=1`
proves the loader groups instances by repo, drops rows without a durable
instance id, keeps the platformless instance, returns no instances for
a repo with none, errors (never silently empties) on a nil graph or a graph read
failure, and that the handler still runs exactly one bounded load per intent. The
same suite also drives the runtime family end to end through the real loader
(`TestServiceCatalogHandlerMaterializesRuntimeFromGraphReads` /
`...EmitsNoRuntimeWhenGraphEmpty`): a correlated service repository whose graph
carries a WorkloadInstance/Platform instance materializes a runtime-family
snapshot row whose generation-stable key is derived from the durable graph
identity, and a repository with no graph instances emits none while ownership
still commits.

Graph-truth vs query-truth: the loader reuses the exact WorkloadInstance and
RUNS_ON platform read shapes the query surface already serves
(`entity_workload_context.go`), so the runtime evidence and the
`get_workload_context`/service-story surfaces draw from the same nodes by
construction. Note on local Compose: the default Compose stack runs only the git
collector and ships no `catalog-info.yaml`/`opslevel.yml`/`cortex.yaml` fixture,
so it produces no `service_catalog_correlation` and cannot exercise the runtime
family locally without a dedicated service-catalog-plus-runtime fixture and a
second re-materialization generation. The live-SQL changed-since gate is the
Postgres integration suite in CI; a full Compose changed-since proof would
require adding that service-catalog runtime fixture, which is out of scope for
wiring the loader.

Observability Evidence: no new metric instrument, label, queue domain, lease, or
runtime knob. The loader runs inside the existing `service_catalog_correlation`
reducer execution span/counter; a read failure surfaces as the wrapped
`load runtime instances for repo <id>` error on the intent's
`fact_work_items` status/failure fields, and the resulting rows are diagnosed
through the same `service_evidence_snapshots` rows and
`get_service_changed_since` read surface as the rest of the runtime family.

## Service dependency evidence family (#1987)

The dependencies evidence family extends the same service materialization lineage
(#1943) without a new table, index, or reducer domain. It shares the deployment
family's source verbatim: the same `resolved_relationships` Postgres path and the
same `RepositoryScopedResolvedRelationshipLoader`
(`DeploymentRelationshipLoader`). On each `service_catalog_correlation` intent,
`ServiceCatalogCorrelationHandler` (when both `MaterializationWriter` and
`DeploymentRelationshipLoader` are wired) loads the correlated services'
repositories' resolved cross-repo relationships in ONE bounded
`GetResolvedRelationshipsForRepos` call, then partitions the result by
relationship type: deployment types (`DEPLOYS_FROM` / `DISCOVERS_CONFIG_IN` /
`PROVISIONS_DEPENDENCY_FOR` / `RUNS_ON`) feed the deployment family and dependency
types (`DEPENDS_ON` / `USES_MODULE` / `READS_CONFIG_FROM`) feed the dependencies
family. Each dependency relationship becomes one dependencies-family
`service_evidence_snapshots` row in the same service generation as ownership,
deployment, and runtime.

The row identity is `ServiceDependencyEvidenceKey(service_id, identity)` =
`dependencies:<service_id>:<identity>`, where `identity` is a sha1 digest of the
relationship's generation-independent natural key
(`relationship_type`, `source_repo_id`, `target_repo_id`, `source_entity_id`,
`target_entity_id`) — the same natural-key contract the deployment family uses.
The relationship's `resolved_id` is deliberately NOT used: `resolved_id` is
`relationships.ResolvedRelationshipID(generation_id, …)`, which digests the
resolution `generation_id` into the id (`relationships/models.go:160-168`), so
keying on it would assign every edge a new key each resolution generation and
report 100% false churn. The natural-key digest keeps the same edge stable across
generations so the FULL OUTER JOIN diff classifies it `unchanged`.

No-Regression Evidence: `go test ./internal/reducer -run
'ServiceDependency|BuildServiceDependency|ServiceMaterialization|ServiceCatalogHandler'
-count=1` proves dependency rows carry generation-stable natural-tuple keys (no
embedded generation/`resolved_id`), a changed payload hash flips the generation
while an identical re-materialization is a no-op, dropped relationships are
tombstoned, the dependencies family is purely additive (no loader leaves the
generation without dependency rows and ownership/deployment/runtime tests stay
green), and both the deployment and dependencies families materialize from the
SAME single bounded relationship load. `go test ./internal/storage/postgres -run
TestComputeServiceChangedSinceDelta -count=1` proves the family-generic delta SQL
(grouped by `evidence_family`) classifies dependencies
added/updated/unchanged/retired/superseded with bounded ordered samples and
reports zero deltas for a non-dependency fixture, never silently dropping it. The
dependency rows reuse the existing `service_evidence_snapshots_diff_idx`
(`generation_id`, `evidence_family`, `service_evidence_key`) the Stage-1 delta
query already drives and the `resolved_relationships` repo-scoped read the
deployment family already uses, so no new index or schema migration is added.
Live-Postgres SQL is proven by the same fake `Queryer`/`Rows` harness Stage-1
uses; the live SQL gate is the Postgres integration suite in CI.

Observability Evidence: this slice adds no new metric instrument, label, queue
domain, lease, or runtime knob. The dependencies family lands inside the existing
`service_catalog_correlation` reducer execution span/counter and the same service
materialization commit path Stage-1 already instruments; operators diagnose it
through reducer run spans, execution counters, `fact_work_items` status/failure
fields, the durable `service_materialization_generations`/
`service_evidence_snapshots` rows, and the `get_service_changed_since`
API/MCP/CLI read surface that now reports the dependencies family alongside
ownership, deployment, and runtime.
