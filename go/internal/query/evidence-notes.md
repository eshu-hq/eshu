# Query Evidence Notes

This file holds issue-specific no-regression, performance, and observability
notes for `internal/query`. Keep the package overview in `README.md`; put longer
evidence records here when they would otherwise push the overview past the
repository line budget.

## Provenance-weighted dead-code incoming-edge probe (#2719)

No-Regression Evidence: `go test ./internal/query ./cmd/api ./cmd/mcp-server
./internal/mcp -count=1` passes after the change; the new
`code_dead_code_provenance_test.go` and `content_reader_dead_code_provenance_test.go`
cases failed to compile/assert before it (they reference the new
`deadCodeIncomingEdge`/`weak_incoming_only` behavior). The incoming-edge probe
was an existence check; it now also reads each edge's `resolution_method` and
derives confidence in Go via `codeprovenance.Confidence`. The graph probe Cypher
returns one extra projected property and drops a `DISTINCT`; the content
read-model SQL changes `UNION` to `UNION ALL` and selects `resolution_method`,
with an outer `SELECT DISTINCT (entity, method)` (and `RETURN DISTINCT` on the
graph probe) bounding row volume to distinct resolution methods per candidate
rather than the raw incoming-edge count. Both run only over the bounded
dead-code candidate batch (functions selected *because* they have few or no
incoming edges), so the rows and the O(edges) max-confidence aggregation add no
round trips, no graph writes, and no measurable regression to the read path. An edge with no recorded
`resolution_method` resolves to `LegacyConfidence` (strong), so the change never
demotes a candidate on unknown provenance.

No-Observability-Change: #2719 reuses the existing `postgres.query`
(`dead_code_incoming_entity_ids`) span and the existing dead-code graph read; it
adds no route, metric, worker, queue, lease, or graph write. The dead-code
response keeps its `classification` field — weak-incoming-only candidates now
classify as `ambiguous` with a `weak_incoming_edge:<method>` ambiguity reason
instead of being silently dropped as reachable.

## Repository tenant-isolation canary (#2048)

No-Regression Evidence: `go test ./internal/query -run 'Test(RepositoryList.*ScopedAuth|ResolveRepositorySelector.*ScopedAuth|ResolveRepositorySelectorDenies|RepositoryListSharedAuth|RepositoryListAllScopeAdmin)' -count=1` failed before the canary because repository list queries had no scoped predicate, content fallback paged before filtering, duplicate-name selectors considered repositories outside the token grant, and canonical out-of-scope IDs resolved successfully. It passed after repository list and selector resolution applied `AuthContext` allowed repository/scope IDs before pagination, count metadata, ambiguity, and not-found decisions.

No-Observability-Change: #2048 changes only the repository read predicate and
in-memory content fallback filter. It adds no route, runtime, worker, queue,
metric name, metric label, graph write, or response field. Operators continue
to diagnose the route through existing `repository_query.stage_*` logs, the
`GraphQuery.Run`/`RunSingle` spans and duration metrics, and the unchanged
repository response `result_limits`, `partial_reasons`, and `truncated`
metadata.

## Scoped route fail-closed gate (#2049)

No-Regression Evidence: `go test ./internal/query -run
'TestAuthMiddlewareWithScopedTokens(RejectsUnsupportedScopedRoute|RejectsScopedMutationOnAllowedPath|AllowsSharedTokenOnUnsupportedRoute|AllowsRepositoryListWithEmptyGrant|RejectsAllScopeOnUnsupportedRoute|AuditsUnsupportedScopedRoute|AttachesAuthContext|PublicPathSkipsResolver)'
-count=1` failed before the route gate because scoped tokens reached
unsupported data routes and non-GET requests on the repository list path. It
passed after the shared auth middleware allowed only explicitly
tenant-filtered scoped routes and returned `permission_denied` before invoking
unsupported handlers. Empty grants can still reach the repository canary, while
all-scope scoped tokens remain fail-closed on unsupported routes. Scoped route
denials also emit validation-safe governance audit events when an audit sink is
wired. Shared-token and public-path behavior remained unchanged.

No-Observability-Change: #2049 changes only pre-handler authorization routing
for scoped bearer tokens. It adds no graph read, content-store read, queue,
worker, runtime knob, response field, metric instrument, or metric label.
Operators can still diagnose denied calls through the existing HTTP status,
structured error envelope, correlation id, request logs, and unchanged route
handler spans for requests that are allowed to reach handlers.

## Repository story capability gate (#3028)

No-Regression Evidence: baseline behavior allowed the repository story handler
to enter repository lookup under `local_lightweight` even though the shared
`platform_impact.context_overview` capability matrix marks that profile
unsupported. The change reuses `requireContextOverview` before any graph or
content read, so unsupported callers now receive the structured capability
error that repository context, workload context, and workload story already
return. After measurement on Go 1.26.4 (`darwin/arm64`) used
`go test ./internal/query -run
'Test(GetRepositoryStory_LocalLightweightReturnsStructuredUnsupportedCapability|GetRepositoryStoryReturnsEnvelopeWhenRequested|GetRepositoryStoryUsesNarrowRepositoryLookupAndLogsStages)'
-count=1`. The focused gate proves zero backend rows for the unsupported
profile because the handler returns before repository selector or story reads;
the supported story path still returns the same bounded repository story
envelope and the existing narrow lookup/log-stage test continues to pass. No
queue, worker, reducer, or graph-write path is involved.

No-Observability-Change: the supported route keeps the existing
`repository_query.stage_*` timers, `Neo4jReader.Run`/`RunSingle` spans,
`neo4j.query` graph spans, response truth envelope, result limits, and partial
reason fields. Unsupported requests now stop earlier with the existing
structured capability error envelope; no metric, log field, span name, route,
runtime knob, queue state, or response field is added.

## Incident context routing evidence (#1142)

Incident context now includes three routing slots before deployable/runtime
promotion:

- `intended_routing` from Terraform-source `PagerDutyDeclaration` content rows.
- `applied_routing` from active
  `incident_routing.applied_pagerduty_resource` facts.
- `live_routing` from active
  `incident_routing.observed_pagerduty_service` facts and scoped
  `incident_routing.coverage_warning` gaps.

The read model keeps source classes separate. Terraform declarations explain
intended routing, Terraform state explains applied routing, and live PagerDuty
facts explain current provider state. These slots do not prove root cause,
service health, deployable identity, image identity, commit, pull request, or
Jira work-item truth. Those later slots still require their existing explicit
service-catalog, reducer, provider-PR, and work-item evidence.

No-Regression Evidence: `go test ./internal/query -run
'TestBuildIncidentRoutingEvidence|TestIncidentContextRoutingQueriesStayBounded'
-count=1` proves exact declared/applied/live convergence, no-IaC live-only
PagerDuty evidence, live drift, permission-hidden coverage warnings,
ambiguous declared routing, and bounded read-model SQL over `content_entities`
plus active incident-routing facts.

Observability Evidence: the route continues to run under
`query.incident_context` with stable route and capability attributes. The new
SQL reads are scoped by incident service id, service-name fingerprint, or active
PagerDuty scope and are covered by the existing Postgres query spans and
`eshu_dp_postgres_query_duration_seconds`; no new high-cardinality metric label
is added.

## Work item evidence reads (#1124)

`WorkItemHandler` exposes `GET /api/v0/work-items/evidence` and backs MCP
`list_work_item_evidence`. The read uses active `work_item.*` source facts from
Postgres and requires `limit` plus at least one anchor:
`scope_id`, `project_key`, `work_item_key`, `provider_work_item_id`,
`external_url`, `url_fingerprint`, or `observed_after`. `external_url` is
sanitized into `url_fingerprint` before SQL, and the response never returns raw
Jira or remote-link URLs.

The route is source-only. It can show exact Jira provider facts, unsupported
remote-link types, missing evidence, stale evidence, permission-hidden rows, or
rejected unsafe payloads. It does not verify pull-request, commit, deployment,
runtime artifact, image, version, service, or incident truth.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -run
'TestWorkItem|TestOpenAPIIncludesWorkItemEvidenceRoute|TestResolveRouteMapsWorkItemEvidenceToBoundedQuery'
-count=1` proves required scopes and limits, URL fingerprinting, cursor
pagination, active fact SQL predicates, OpenAPI exposure, and MCP dispatch.

Observability Evidence: the route runs under `query.work_item_evidence` with
stable route and capability attributes plus bounded `SpanAttrWorkItemEvidence*`
span attributes for
`eshu.query_count`, `eshu.result_count`, `eshu.stale_evidence_count`,
`eshu.permission_hidden_count`, `eshu.rejected_unsafe_payload_count`,
`eshu.unsupported_link_type_count`, `eshu.missing_evidence_count`, and
`eshu.truncated`. The bounded Postgres read is also covered by `postgres.query`
spans and `eshu_dp_postgres_query_duration_seconds`; no raw URL, issue summary,
user, or tenant value is added to metric labels.

## Supply-chain impact catalog anchors (#1668)

Supply-chain impact findings expose `catalog_entity_refs[]` and
`catalog_owner_refs[]` separately from `service_ids[]` and `workload_ids[]`.
Repository-scoped exact service-catalog evidence can explain ownership and
catalog identity without fabricating an operational service or workload anchor.

No-Regression Evidence: `go test ./internal/query -run
'Test(SupplyChainListImpactFindingsExposesOperationalAnchors|SupplyChainExplainImpactExposesOperationalAnchors|DecodeSupplyChainImpactFindingRowPreservesCatalogAnchors)'
-count=1` proves list responses, explain anchors, and Postgres payload
hydration preserve catalog entity/owner anchors while keeping missing-evidence
reasons intact.

No-Observability-Change: this is a response-shape and decode extension over the
existing bounded `query.supply_chain_impact_findings` and
`query.supply_chain_impact_explain` Postgres reads. It adds no route, graph
query, queue, worker, runtime knob, metric instrument, metric label, or new
high-cardinality filter; operators continue to diagnose the path through the
existing query spans, Postgres query duration metrics, truth envelope, and
durable reducer payload fields.

## Package registry aggregate hot-path evidence (#689)

The graph-backed package-registry aggregate (`package_registry_aggregates.go`,
`package_registry_aggregates_handler.go`) is the first aggregate in this
package that emits Cypher rather than SQL, so `scripts/verify-performance-evidence.sh`
flags the file via its hot-path-by-content check. The Cypher follows the
NornicDB-New hot-path query cookbook Area 5 "Grouped Count" and
`PatternOutgoingCountAgg` shapes verbatim: `MATCH (p:Package) WHERE
p.<indexed_prop> = $value RETURN coalesce(p.<group>, 'unknown') AS bucket,
count(p) AS bucket_count ORDER BY bucket_count DESC SKIP $offset LIMIT $limit`.

No-Regression Evidence: `go test ./internal/query -run
'TestPackageRegistryAggregate|TestPackageRegistryInventoryGroupExpression|TestNextPackageRegistryAggregateOffset|TestGraphPackageRegistryAggregateStore'
-count=1` proves the production Reader emits Cypher with the cookbook
hot-path shape: `MATCH (p:Package)` label-property anchor,
indexed-property predicate, deterministic ordering, parameter-bound limit,
and a closed-enum dimension map so the substituted group expression stays
parameter-safe. The indexes the hot path depends on
(`go/internal/graph/schema.go`: `package_registry`, `package_namespace`,
`package_package_manager`, `package_visibility`) ship in the same PR; the
long-standing `package_ecosystem` index covers the default grouping.
Operators applying this PR must re-run `eshu-bootstrap-data-plane` so the
four new indexes exist before the aggregate routes resolve in production. A
`PROFILE` proof against the pinned NornicDB binary is the operator gate for
promoting the routes. The in-process tests guard the Cypher shape, but
`PROFILE` is the only definitive evidence that the planner picks the indexed
seek; capture it after `eshu-bootstrap-data-plane` completes.

Observability Evidence: the aggregate routes add the
`query.package_registry_aggregate` request span registered in
`go/internal/telemetry/contract_package_registry.go` with route and
capability attributes. They re-use the existing `Neo4jReader.Run` tracing
and the `neo4j.query` graph span; no new metric instrument is added.

## Infra resource cloud search evidence (#1400)

The infrastructure search route includes canonical `CloudResource` nodes in
the same bounded `POST /api/v0/infra/resources/search` graph query used for
Terraform, Kubernetes, Argo CD, Crossplane, Helm, and CloudFormation resources.
`category=cloud` narrows the label predicate to `CloudResource`. Generic
search text checks stable cloud identity fields (`arn`, `resource_id`,
`resource_type`, `name`, `service_kind`, account, and region). Provider
filters and response shaping use `source_system` only as a CloudResource
fallback; non-cloud nodes that carry provenance values such as
`terraform_state` do not surface those values as cloud providers.
Resource-service filters still map to `service_kind` for CloudResource rows
that have no Terraform-style `resource_service` property. The result shape
keeps cloud identity fields visible while leaving raw tag maps and reducer
evidence payloads out of the generic search response.

No-Regression Evidence: `go test ./internal/query -run
'TestSearchInfraResources|TestInfraResourceAggregate|TestInfraResourceInventoryGroup|TestGraphInfraResourceAggregate'
-count=1` proves CloudResource label selection, provider/resource-service
filter mapping, explicit multiple-candidate results, redaction of raw tags and
evidence payloads, truncation, and the aggregate category guard.

No-Observability-Change: the route still runs under
`query.infra_resource_search` with stable `http.route` and `eshu.capability`
span attributes, existing `Neo4jReader.Run` tracing, and the `neo4j.query`
graph span. No collector, reducer, graph write, queue worker, metric
instrument, or deployment knob changes.

## Infra resource structured-filter search evidence (#1599)

The infrastructure search route accepts a filter-only request when at least
one bounded structured scope is present: `category`, `kind`, `provider`,
`environment`, `resource_service`, or `resource_category`. Empty or
whitespace-only requests still fail before the graph read. Successful requests
always carry the existing defaulted and capped `LIMIT` parameter plus the
one-extra-row truncation probe.

Filter-only requests preserve the same direct label predicate used by text
search, then add equality filters for the supplied structured scope without
emitting the generic `CONTAINS $query` free-text predicate. Cloud searches keep
the existing provider and service fallback rules: `source_system` is considered
a provider only for `CloudResource`, and `service_kind` backs
`resource_service` for cloud rows that lack Terraform-style properties.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -run
'TestSearchInfraResources|TestOpenAPIInfraSearchAllowsStructuredFilterScope|TestResolveRouteFindInfraResourcesPreservesStructuredFilters|TestFindInfraResourcesToolSchemaAllowsStructuredFiltersWithoutQuery'
-count=1` proves structured-filter-only HTTP search, unbounded-request
rejection, OpenAPI request schema, and MCP routing/schema parity.

No-Observability-Change: the route still runs under
`query.infra_resource_search` with stable `http.route` and `eshu.capability`
span attributes, existing `Neo4jReader.Run` tracing, and the `neo4j.query`
graph span. No collector, reducer, graph write, queue worker, metric
instrument, or deployment knob changes.

## Resource investigation cloud candidate evidence

No-Regression Evidence: `go test ./internal/query -run
'TestInvestigateResourceResolvesExactCloudARN|TestBuildServiceStoryTraceExplainsUncorrelatedCloudCandidates|TestLoadUncorrelatedCloudResourceCandidatesUsesBoundedServiceSelector|TestBuildDeploymentTraceResponseExplainsUncorrelatedCloudCandidates|TestLoadResourceInvestigationSectionsJoinsParallelErrors'
-count=1` proves resource investigation accepts exact cloud ARNs returned by
infra search, section traversals keep the canonical graph id and ARN handles,
and service story/deployment trace expose bounded uncorrelated cloud-resource
candidates without promoting them into canonical `cloud_resources`.

Remote Docker Compose proof against NornicDB verified the same CloudResource
candidate path on a target service with six AWS cloud-resource matches. The
service-story stage returned zero rows with a broad multiline `OR` predicate
and direct optional property projections, then returned all six candidates with
the one-line `infraResourceFreeTextPredicate` shape and coalesced optional
projections. The API and MCP service-story readbacks both kept canonical
`cloud_resources` empty and surfaced those rows only as
`uncorrelated_cloud_resources` with `missing_relationship` set to
`workload_cloud_relationship`.

No-Observability-Change: the candidate read still runs through
`GraphQuery.Run`, existing `neo4j.query` spans, and
`eshu_dp_neo4j_query_duration_seconds`; service-story enrichment records the
`uncorrelated_cloud_resource_candidates` stage through existing
`service_query.stage_started` and `service_query.stage_completed` log events.

## Deployment trace config-derived cloud resources

Deployment trace may promote CloudResource rows into `cloud_resources` from
config-read evidence only when deployment evidence includes an explicit
`READS_CONFIG_FROM` artifact. This keeps config-backed resources visible in the
code-to-cloud story while preserving the stricter uncorrelated candidate
contract for name or ARN matches that lack a workload-to-cloud relationship.

No-Regression Evidence: `go test ./internal/query -run
'TestTraceDeploymentChainKeepsConfigDerivedCloudResources|TestConfigDerivedCloudResourceDependenciesRequireConfigReadEvidence'
-count=1` failed before the trace preserved workload deployment evidence and
loaded config-derived CloudResource rows, then passed after explicit config-read
evidence produced `relationship_basis=deployment_config_read_evidence`.

No-Observability-Change: config-derived resource reads use the existing
`GraphQuery.Run` adapter and query telemetry, and the deployment-trace handler
continues to expose the result through the existing `cloud_resources`,
`deployment_overview`, and `deployment_fact_summary` response fields.

## Infra resource aggregate hot-path evidence (#690)

The graph-backed infrastructure resource aggregate
(`infra_resource_aggregates.go`, `infra_resource_aggregates_handler.go`)
emits the same Area 5 "Grouped Count" Cypher shape as the package-registry
aggregate, narrowed across the existing infra label families instead of the
single `Package` label. The handler resolves a single optional `category`
input (`k8s`, `terraform`, `argocd`, `crossplane`, `helm`, `cloud`) to a closed
label set via `resolveInfraLabels`, then emits `MATCH (n) WHERE n:<Label1> OR
n:<Label2> ... [AND n.<indexed_prop> = $value] RETURN <bucket_expr> AS
bucket, count(n) AS bucket_count ORDER BY bucket_count DESC SKIP $offset
LIMIT $limit`. Bucket normalization uses the `CASE WHEN n.x IS NULL OR
n.x = '' THEN 'unknown' ELSE n.x END` form so empty-string properties land
in the `unknown` bucket alongside true NULLs.

No-Regression Evidence: `go test ./internal/query -run
'TestInfraResourceAggregate|TestInfraResourceInventoryGroup|TestGraphInfraResourceAggregate|TestNextInfraResourceAggregateOffsetBound'
-count=1` proves the production Reader emits the cookbook hot-path shape
across the resolved label set, deterministic `ORDER BY bucket_count DESC`,
parameter-bound `SKIP`/`LIMIT`, and a closed-enum dimension map so the
substituted group expression stays parameter-safe. The
`TestGraphInfraResourceAggregateCountShapeNarrowsToCategoryLabels` test
guards the label narrowing: `category=terraform` produces Cypher matching
`TerraformResource` but not `K8sResource`, and `category` omitted produces
the full label union.

The indexes the hot path depends on ship in the same PR
(`go/internal/graph/schema.go`): `tf_resource_provider`,
`tf_resource_environment`, `tf_resource_service`, `tf_resource_category` on
the `TerraformResource` label, which is the dominant infra fact source.
Property predicates use direct equality (`n.provider = $provider`) rather
than `coalesce(n.provider, '') = $provider`; coalesce around an indexed
property would block planner index selection. The
`TestInfraResourceAggregateWhereClauseUsesDirectEqualityForIndexedProps`
test guards this, so a future refactor that reintroduces coalesce in the
WHERE clause fails the suite. K8sResource exposes `k8s_kind`, so
`category=k8s` plus `kind=<value>` is the other supported hot path today.
`category=cloud` narrows to canonical `CloudResource` nodes; provider filters
map to CloudResource `source_system`, while the all-category provider fallback
is gated by `n:CloudResource` so Terraform-state provenance does not create
fake provider buckets such as `terraform_state`. Resource-service filters map
to the CloudResource `service_kind` property when Terraform-style fields are
absent. Aggregates over Argo CD, Crossplane, Helm, CloudResource, or
CloudFormation labels still answer but fall back to a label-set scan until
matching indexes ship. A `PROFILE` proof against the pinned NornicDB binary is
the operator gate for promoting the routes once `eshu-bootstrap-data-plane`
re-runs the schema apply step. The in-process tests guard the Cypher shape, but
only `PROFILE` proves the planner picks the new TerraformResource indexes.

Observability Evidence: the aggregate routes add the
`query.infra_resource_aggregate` request span registered in
`go/internal/telemetry/contract.go` with route and capability attributes.
They re-use the existing `Neo4jReader.Run` tracing and the `neo4j.query`
graph span; no new metric instrument is added.

## Ecosystem overview counts

`GET /api/v0/ecosystem/overview` counts repositories, workloads, platforms, and
workload instances. Each label is counted with its own bounded single-label
count query (`MATCH (x:Label) RETURN count(x)`), not a single chained
`MATCH ... WITH count() MATCH ... WITH count()` statement. Chained cross-MATCH
aggregation is not portable on NornicDB: an empty intermediate label collapsed
the whole result and zeroed `repo_count`, and the chained form otherwise
returned multiple all-null rows. A bare single-label count returns a single `0`
row when the label is empty on both backends, so the repository count never
disappears because workloads or platforms are not yet materialized.

No-Regression Evidence: query shape changed from one chained-aggregation
statement to four independent single-label count scans (NornicDB v1.1.3, local
Docker Compose, `~/example-repos` corpus: 33 Repository, 21 Workload, 7 Platform, 92
WorkloadInstance nodes). Each scan is the same bounded label count as the
original; the change adds three extra round-trips on a low-frequency overview
read and removes none of the original scan work, so there is no scan-cost
regression. Runtime before/after on the same stack: `GET /api/v0/ecosystem/overview`
returned `{repo_count:0,workload_count:0,platform_count:0,instance_count:0}`
before (chained statement collapsed despite 33 indexed repositories) and
`{repo_count:33,workload_count:21,platform_count:7,instance_count:92}` after.
Regression guard: `TestGetEcosystemOverviewCountsEachLabelIndependently`.

No-Observability-Change: the route keeps the same `Neo4jReader.RunSingle` /
`neo4j.query` graph spans and the same response field shape; no new metric, log
field, or span is added.

## Container image (OCI) inventory list (#1645)

`GET /api/v0/images` lists container images over the authoritative
`(:ContainerImage)` graph. The anchor is a single label scan over
`(:ContainerImage)`, bounded by `limit+1` (default 50, max 200) with optional
exact filters on `digest`, `repository_id`, and `source_tag`, deterministic
`ORDER BY img.digest, img.uid`, and offset-based continuation via `next_cursor`.
`(:ContainerImage)` is the small image-inventory label, not the per-layer
descriptor population, so a bounded label scan is the correct shape rather than
an unindexed property anchor. The handler surfaces node properties only; in the
current graph `ContainerImage` nodes carry no workload edges (`DEPLOYS_FROM` is
Repository->Repository), so the list never fabricates a deploying-workload
column.

Performance Evidence: measured the exact handler Cypher shape against the warm
local Compose backend (NornicDB, `nornic` database, `~/example-repos` corpus,
10 `ContainerImage` nodes) over the Bolt-HTTP tx endpoint with `limit+1=51`,
`offset=0`, and empty filters. Warm priming call: 3.2 ms; three measured runs:
0.82 ms, 0.71 ms, 1.02 ms wall time. The bounded label scan returns the full
10-row inventory well inside the sub-second band the histogram buckets expect.
The running API on `127.0.0.1:8080` already exposes the route
(`GET /api/v0/images?limit=5` returns `401 unauthenticated` in 2.7 ms, not
`404`), so the route is mounted; the load-bearing latency proof is the
query-level measurement above.

Observability Evidence: the handler emits the `query.container_image_list` span
(`SpanQueryContainerImageList`) with `http.route` and
`eshu.capability=platform_impact.container_image_list` attributes, the
`eshu_dp_query_image_list_duration_seconds` histogram with a low-cardinality
`outcome` label (`ok`, `invalid_request`, `query_error`,
`unsupported_capability`, `backend_unavailable`), and the
`eshu_dp_query_image_list_errors_total` counter with a bounded `reason` label.
Responses carry `limit`, `offset`, `truncated`, and `next_cursor` so a slow or
incomplete page is diagnosable from traces and payload alone.

No-Regression Evidence: this PR adds a new bounded read; it changes no existing
query, reducer, queue, or graph-write path. `go test ./internal/query -run
'Image' -count=1` covers bounds parsing, truncation, filter pass-through, the
registry/repository split, and the OpenAPI fragment.

## Ask SSE validated-token contract (#3322)

Ask SSE token events are a validated narration surface, not a raw provider
stream. `handleAskSSE` forwards only `AskStream` token events, and the engine
emits those events only after the narration validator accepts the prose. The
public HTTP docs and OpenAPI fragment keep that contract explicit.

No-Regression Evidence: `go test ./internal/query -run 'TestAskSSE|TestOpenAPIAsk'
-count=1` covers SSE forwarding, synchronous fallback, leak-safe error
handling, and the route documentation fragment.

No-Observability-Change: this changes only response emission timing/content;
existing HTTP status, SSE `trace`/`answer`/`error` events, Ask engine warnings,
query trace entries, and provider adapter logs remain the diagnostic surface.

## Bounded catalog workload enrichment (#3389)

`GET /api/v0/catalog` assembles bounded workload handles by joining three
per-workload graph enrichments (repository, instance environments, and
deployment-evidence environments) onto the `limit+1` workloads the base query
returns. Before this change the three enrichment queries
(`catalogWorkloadRepoCypher`, `catalogWorkloadInstanceEnvironmentCypher`,
`catalogWorkloadEvidenceEnvironmentCypher` in
`catalog_workload_environments.go`) ran with no parameters and no bound, so each
scanned and aggregated the entire `Workload`, `WorkloadInstance`, and
`Environment` populations regardless of the requested `limit`. At the issue's
502,865-node scale (full cloud/SaaS collector data) that whole-graph aggregation
timed the endpoint out (HTTP 000 at 20-30s) even at `limit=5`, because the cost
is driven by the graph size, not the page size. This is the same read-path-scale
class as the uncorrelated `CloudResource` scan bounded in #3378.

The fix passes the bounded workload id set (`$ids`) from the base query into
each enrichment and anchors every enrichment on `(w:Workload) WHERE w.id IN
$ids`, the indexed bounded-id lookup shape used by `repository_name_lookup.go`
and `entity_workload_context.go` and backed by the `nornicdb_workload_id_lookup`
index on `Workload.id`. An empty id set short-circuits all three graph round
trips. The kept workloads, their joined repository/instance/environment facts,
and the truncation semantics are identical to the previous shape; the only
change is that the enrichments visit at most `limit` workloads instead of the
whole graph.

No-Regression Evidence: the bound is the issue's root cause, not a result-size
cap. The three enrichments are now registered hot paths in the static
query-plan gate (`QP-DEPLOY-CATALOG-ENV`, `QP-DEPLOY-CATALOG-WORKLOAD-REPO`,
`QP-DEPLOY-CATALOG-WORKLOAD-INSTANCE` in
`go/internal/queryplan/testdata/hot-cypher.yaml`), each declaring the
`Workload.id` anchor, the `nornicdb_workload_id_lookup` schema evidence, and a
plan that forbids `AllNodesScan`/`CartesianProduct`/`UnboundedExpand`;
`go test ./internal/queryplan -count=1` enforces those shapes. Handler behavior
is covered by `go test ./internal/query -run Catalog -count=1`
(`TestListCatalogBoundsEnrichmentQueriesToWorkloadIDs` asserts each enrichment
carries the bounded `$ids` set and the `WHERE w.id IN $ids` anchor,
`TestListCatalogSkipsEnrichmentWhenNoWorkloads` asserts no enrichment round trip
on an empty catalog, and the existing merge/truncation tests assert the joined
output is unchanged). A live 500k-node wall-clock capture was not run in this
environment; the load-bearing proof is the static plan-shape gate plus the
elimination of the unbounded label aggregation.

Observability Evidence: No-Observability-Change. The catalog handler keeps its
existing response surface (`limit`, `truncated`, `counts`, per-collection rows
and the catalog truth envelope). No span, metric, log, status row, graph write,
or queue consumer is added or removed; the change only narrows the parameters
and anchor of three existing read queries.

## Supply-chain aggregation endpoints (#3389)

Three supply-chain read endpoints aggregated `fact_records` for a single
`fact_kind` and timed out (>20-30s) once the cloud/SaaS collectors grew the stack
to ~502,865 graph nodes and inflated `fact_records` to collector scale:

- `GET /api/v0/supply-chain/advisories`
  (`supply_chain_advisory_catalog_sql.go`): the `cve_facts` CTE (catalog spine)
  enumerates every active `vulnerability.cve` fact, the `affected` CTE enumerates
  every active `vulnerability.affected_package` fact, and the `kev` CTE enumerates
  every active `vulnerability.known_exploited` fact, each with no `cve_id` anchor,
  before `GROUP BY advisory_key` and keyset pagination.
- `GET /api/v0/supply-chain/impact/findings/count`
  (`supply_chain_impact_aggregates_queries.go`): the shared `scoped_facts` CTE
  enumerates every active `reducer_supply_chain_impact_finding` fact, then
  `ranked_facts` dedupes with `ROW_NUMBER() OVER (PARTITION BY canonical_key ...)`
  before the count/group rollups.
- `GET /api/v0/supply-chain/sbom-attestations/attachments/count`
  (`sbom_attestation_attachment_aggregates.go`): a global `COUNT(*)` plus two
  `GROUP BY` aggregates over every active `reducer_sbom_attestation_attachment`
  fact.

In the common "count/list everything" case the per-payload filters are all
no-ops, so there is no payload anchor. The earlier evidence note hypothesized
this needed a maintained summary table because "a covering index does not bound a
global rollup/count." That framing conflated two different index properties. A
*covering* (payload-leading) index does not help here: with no predicate on the
leading payload expression, the planner falls back to a sequential scan of all of
`fact_records` (every `fact_kind`, ~collector-scale rows) or a full scan of a
wide payload-leading index. A *partial* index is categorically different: its
`WHERE` clause is baked into the index contents, so the index physically holds
only the rows that satisfy the predicate. Scanning it — even fully — is therefore
bounded to that predicate's row set.

The fix adds one partial index per aggregated fact_kind whose predicate is
exactly the query's fact-kind bound and whose key columns are the
active-generation join keys:

```sql
CREATE INDEX IF NOT EXISTS fact_records_<kind>_active_scan_idx
    ON fact_records (scope_id, generation_id, fact_id ASC)
    WHERE fact_kind = '<kind>' AND is_tombstone = FALSE;
```

for `<kind>` in `vulnerability.cve`, `vulnerability.affected_package`,
`vulnerability.known_exploited`, `reducer_supply_chain_impact_finding`, and
`reducer_sbom_attestation_attachment`
(`schema_fact_records_vulnerability_indexes.go`, `schema_fact_records.go`,
`schema_fact_records_sbom.go`). The partial predicate carves the scan down from
"all of `fact_records`" to "this one fact_kind's active, non-tombstone tuples,"
which is the bound the aggregate needs. The `(scope_id, generation_id)` leading
key columns are the exact join keys to `scope_generations` / `ingestion_scopes`,
so the planner can drive the active-generation join from the index (a merge join
that supplies both join columns without a heap trip, or a hash join fed by the
same bounded index scan). The scan becomes index-only when the heap pages are
all-visible in the visibility map (vacuum-fresh); on a write-heavy table it may
take heap fetches, but it stays bounded to the fact_kind either way. No payload
columns are covered because the count/group expressions are JSONB and vary per
aggregate (count vs priority bucket vs severity bucket vs subject_digest); the
load-bearing property is the partial predicate, not coverage.

No GIN index was added for the `payload->'..._ids' ?| $n` array-containment
source-scope filters: those already have `..._repository_anchor_idx` /
`..._workload_anchor_idx` / `..._service_anchor_idx` GIN indexes, and adding more
would add write amplification (per the #3383 write-amplification caveat) for a
path the new btree partial index already bounds.

Accuracy: the aggregates are byte-for-byte unchanged. The fix only adds indexes;
the SQL fact-kind anchor, `is_tombstone = FALSE` filter, active-generation join,
dedupe ranking, grouping, ordering, and pagination are all untouched, so result
truth is identical and the indexes only change the access path.

Performance Evidence: this environment has no provisioned ~500k Postgres stack,
so a live `EXPLAIN (ANALYZE)` wall-clock capture was not run (stated per
cypher-query-rigor; the no-live-backend posture matches #3380/#3384). The
load-bearing proof is the partial-predicate bounding argument above plus the
query-shape and index-presence tests, which together pin that (a) each aggregate
keeps the single-fact_kind + `is_tombstone = FALSE` + active-generation predicate
the index is built on, and (b) the matching partial index DDL exists in the
bootstrap schema. Before: planner has no bounded access path for the no-anchor
case and scans all of `fact_records` (collector scale). After: the planner has a
partial index containing exactly the fact_kind's active tuples, ordered on the
join keys. To finalize on a live stack, run `EXPLAIN (ANALYZE, BUFFERS)` on each
aggregate with all payload params `''` and confirm the plan node is an Index
Scan / Index Only Scan on the new `..._active_scan_idx`, not a Seq Scan on
`fact_records`, after `ANALYZE fact_records`.

Tests: `TestAdvisoryCatalogQueryKeepsPerFactKindActiveScanAnchor`,
`TestSupplyChainImpactAggregateQueriesKeepActiveScanAnchor`,
`TestSBOMAttestationAttachmentAggregateQueriesKeepActiveScanAnchor`
(`internal/query`) pin the bounded query shape;
`TestBootstrapDefinitionsIncludeAdvisoryCatalogActiveScanIndexes`,
`TestBootstrapDefinitionsIncludeSupplyChainImpactFactIndexes`,
`TestBootstrapDefinitionsIncludeSBOMAttestationAttachmentFactIndexes`
(`internal/storage/postgres`) pin the index DDL. Per-fact_kind index/scale
detail is in
`go/internal/storage/postgres/evidence-supply-chain-aggregate-index-scale.md`.

Observability Evidence: No-Observability-Change. The response surfaces, truth
envelopes, HTTP request metrics, and `postgres.query` spans/duration metrics are
unchanged; the fix only changes which rows the aggregate scans, not what the
endpoints emit.

### Endpoint 1 — advisory catalog single-pass reshape (this PR)

`GET /api/v0/supply-chain/advisories` (`supply_chain_advisory_catalog_sql.go`).

The #3402 partial indexes above bound each per-fact_kind *scan*. They do not, and
cannot, bound the catalog's second cost center: the original query built three
`MATERIALIZED` CTEs and `LEFT JOIN`ed two whole-fact-kind aggregates (`catalog`
over `vulnerability.cve`, `affected_rollup` over `vulnerability.affected_package`)
on a computed `advisory_key`. Postgres estimates that grouped, expression-keyed
input at `rows=1`, so both rollup joins plan as `Nested Loop Left Join`. At high
advisory cardinality that is an O(active_facts^2) join an index cannot touch. This
PR removes the join: `vuln_facts` reads the three kinds as per-kind `UNION ALL`
legs (each leg keeps its single `fact_kind` + `is_tombstone = FALSE` predicate, so
the #3402 `*_active_scan_idx` partial indexes stay eligible) and one
`GROUP BY advisory_key` with per-kind `FILTER`ed aggregates rolls them up.
`HAVING bool_or(fact_kind = 'vulnerability.cve')` preserves the cve-spine identity
of the previous `catalog` LEFT JOIN.

Measurement environment: isolated throwaway Postgres 18 (`postgres:18-alpine`,
`work_mem=16MB`, `fact_records` DDL/partial indexes including the #3402
`*_active_scan_idx` indexes applied via schema-only `pg_dump` + the merged DDL),
seeded with a production-shaped synthetic corpus: 250,000 `vulnerability.cve` +
250,000 `vulnerability.affected_package` + 30,000 `vulnerability.known_exploited`
in one vulnerability-intelligence scope, across 901 active scopes (1.53M active
facts total). The live stack's own supply-chain fact kinds are too small (9 CVE /
15 affected / 44 attachment / 64 finding rows) to exercise the join blowup, so a
representative seed was required. This is the high-advisory-cardinality regime
(e.g. full-NVD vuln-intel ingestion); it is distinct from the scan-bound regime
#3402 measured, and both fixes are needed for the catalog to stay bounded.

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` of the shipped
`listAdvisoryCatalogQuery` (no-filter first page, `work_mem=16MB`), with the
#3402 `*_active_scan_idx` indexes present in both arms:

| Run | Before — original MATERIALIZED-CTE shape (with #3402 indexes) | After — per-kind UNION ALL + single GROUP BY |
| --- | --- | --- |
| 1 | did not complete — still `Nested Loop Left Join (rows=1)`; cancelled at the 600s statement timeout | 5.10 s |
| 2 | (same plan) | 4.31 s |
| 3 | (same plan) | 4.91 s |

- Before plan (even with #3402 indexes applied + `ANALYZE`): the per-kind scans
  use the new `*_active_scan_idx` / `scope_generation` indexes, but the
  `catalog` → `affected_rollup` → `kev` step is still `Nested Loop Left Join
  (rows=1, actual ~250k)`. Proof that an index cannot bound the join.
- After plan: `GroupAggregate` over an `Append` of the three per-kind legs (530k
  rows in ~1.2s); the `vulnerability.known_exploited` leg uses
  `fact_records_vulnerability_known_exploited_active_scan_idx` and the
  cve/affected legs use the equally-bounded `fact_records_scope_generation_idx`.
  No aggregate-to-aggregate join node exists, so the nested-loop blowup is
  structurally impossible.

Classification: wall-clock win that removes an unbounded-with-scale
(O(active_facts^2)) cost. Residual cost is one O(active vulnerability facts)
aggregate pass; its `GROUP BY`/`ORDER BY` spills to an external merge
(`Disk: 53MB` at 250k advisories) like the pre-#3375 collector-readiness
aggregate. Driving this below a few seconds would need a maintained per-advisory
summary table (O(page) keyset read), deliberately deferred because it adds a
writer/staleness contract; this reshape already converts a hard timeout into a
working browse with no new moving parts.

No-Regression Evidence: output is byte-identical to the previous shape. The
shipped query string (param-substituted for the first page) and the original
query were each run via `COPY (...) TO STDOUT WITH (FORMAT csv)` on an identical
300-advisory fixture; full ordered output `diff` is empty (300 rows). Filtered
cases were diffed the same way and are byte-identical: `kev=true` (60 rows),
`severity=critical` (75), `ecosystem=npm` (60, an affected-package-derived
filter), and a keyset cursor `after_cvss=5.0 / after_advisory_key=CVE-2024-0000150`
(151). Unit coverage: `TestAdvisoryCatalogQueryUsesBoundedSinglePassShape` pins
the per-kind-UNION-ALL + single-GROUP-BY shape and guards against reintroducing
`AS MATERIALIZED` / `LEFT JOIN affected_rollup`;
`TestAdvisoryCatalogQueryKeepsPerFactKindActiveScanAnchor` (from #3402) still
passes because each leg keeps its single-fact_kind + `is_tombstone` + active-gen
anchor that the partial indexes need.

No-Observability-Change: only the `listAdvisoryCatalogQuery` SQL text changed.
The route still emits the `query.advisory_catalog` span
(`telemetry.SpanQueryAdvisoryCatalog`), the Postgres query-duration histogram,
HTTP status/error bodies, the truth envelope, and `count` / `limit` /
`truncated` / `next_cursor`. No metric, span, log, status row, graph write, or
queue consumer is added or altered. The eliminated nested-loop cost and the
residual external-merge sort are visible to operators through the same endpoint
latency and Postgres `temp_bytes` / `pg_stat_database` counters.

Wire contract unchanged: response shape, ordering (`cvss_score DESC,
advisory_key ASC`), keyset cursor, and truncation are identical, so no OpenAPI or
HTTP API reference change is required.

### Endpoint 3 — sbom attachment count single-pass rollup (this PR)


`GET /api/v0/supply-chain/sbom-attestations/attachments/count`
(`sbom_attestation_attachment_aggregates.go`).

#3402's `fact_records_sbom_attestation_attachments_active_scan_idx` bounds the
scan, but the count handler still issued **three** queries over those active
tuples — one `COUNT(*)` plus two `GROUP BY` (per attachment_status, per
artifact_kind) — so it paid three full scans and three round trips. This PR folds
all three into one `GROUP BY GROUPING SETS` scan
(`sbomAttestationAttachmentAggregateRollupQuery`); the `GROUPING()` flags tag each
row as a status bucket, a kind bucket, or the grand total, and
`buildSBOMAttestationAttachmentAggregateCount` partitions them in Go. The
single-fact_kind + `is_tombstone = FALSE` + active-generation anchor is unchanged,
so the #3402 partial index stays eligible.

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` against the seeded Postgres 18
corpus (500,000 active `reducer_sbom_attestation_attachment` facts across 900
scopes, `work_mem=16MB`, #3402 indexes present), count-everything case (all
payload filters empty):

| | Before — COUNT(*) + 2 GROUP BY (3 queries) | After — one GROUPING SETS scan |
| --- | --- | --- |
| query 1 (COUNT) | 138 ms (Index Only Scan on `*_active_scan_idx`) | — |
| query 2 (GROUP attachment_status) | 1012 ms | — |
| query 3 (GROUP artifact_kind) | ~1012 ms | — |
| total wall (3 round trips) | ~2.16 s | — |
| single rollup (1 round trip) | — | 1.19 s / 0.71 s / 0.81 s |

The after plan is one `MixedAggregate` over a single bounded scan
(`fact_records_scope_generation_idx`, 901 scope loops), no external-merge spill.
Classification: wall-clock win (~2-3x) plus two fewer round trips. The reducer
write path is untouched.

No-Regression Evidence: the rollup partitioning is byte-identical to the prior
trio. On a 500-attachment fixture, the GROUPING SETS rows were diffed against the
separate `COUNT(*)` and two `GROUP BY` queries: grand total (500), every
attachment_status bucket (e.g. `attached_parse_only` 72, `attached_verified` 71,
…), and every artifact_kind bucket (`sbom` 250, `attestation` 250) match exactly.
Unit coverage: `TestBuildSBOMAttestationAttachmentAggregateCount` pins the
grouping-flag partition (and that the rolled-up grand-total row never leaks into a
bucket map); `TestSBOMAttestationAttachmentAggregateRollupUsesSinglePassGroupingSets`
pins the GROUPING SETS shape;
`TestSBOMAttestationAttachmentAggregateQueriesKeepActiveScanAnchor` (from #3402)
still passes because the rollup keeps the per-kind + `is_tombstone` +
active-generation anchor.

No-Observability-Change: the count handler's response (`total_attachments`,
`by_attachment_status`, `by_artifact_kind`, `missing_evidence`, `scope`), HTTP
request metrics, and `postgres.query` spans/duration metrics are unchanged. The
missing-evidence probe is untouched. No metric, span, log, status row, graph
write, or queue consumer is added or altered; only the count handler's three
queries collapse to one.

## Dependency-repo marker from inbound DEPENDS_ON edge (#3394)

Context: the `GET /api/v0/repositories` and `GET /api/v0/catalog` list queries
returned `coalesce(r.is_dependency, false)`, probing a Repository node property
that no writer ever sets. `is_dependency` is a file/entity parser flag written
onto File nodes (`internal/graph/batch.go`, `internal/collector/git_fact_builder.go`),
never onto Repository nodes, so the console "Dependency repos" tile was always 0.
The fix replaces the phantom property read with `repositoryDependencyMarkerProjection`
in `internal/query/neo4j.go`, which marks a repo as a dependency when it is the
target of an admitted `(:Repository)-[:DEPENDS_ON]->(:Repository)` edge.

Accuracy: a "dependency repo" is now exactly a repository other repositories
depend on, backed by the admitted repo-to-repo `DEPENDS_ON` edge that the
repo-dependency projection lane already materializes (runtime-services evidence,
`reason = 'Runtime services list declares repository dependency'`). No new edge,
node property, or admission path is introduced, so no correlation truth is
invented. Package-evidenced consumer->publisher repo edges remain deferred
(see below) because the publisher side is intentionally provenance-only.

Tenant-isolation correctness: for scoped callers the `repositoryDependencyMarkerProjection`
function applies `repositoryAccessFilter.graphPredicate` to the inner (depending)
Repository node inside the EXISTS block. This prevents a scoped caller from
learning dependency-marker truth about in-scope repositories derived from
depending nodes outside their grant. The `$allowed_repository_ids` and
`$allowed_scope_ids` parameters are already bound by `access.graphParams` in
the outer query so no extra round-trip or parameter injection is needed.
For shared/admin/local callers (`allScopes: true`) no predicate is added.
`TestListRepositoriesScopedDependencyMarkerFiltersDepender` asserts the EXISTS
block contains `$allowed_repository_ids` when the request carries a scoped token.

No-Regression Evidence: `go test ./internal/query ./cmd/api ./cmd/mcp-server
./internal/mcp -count=1` passes (3801 tests) after the change. The new
`repository_list_dependency_marker_test.go` pins that the list query derives
`is_dependency` from an inbound `<-[:DEPENDS_ON]-` existence check, no longer
reads `coalesce(r.is_dependency`, surfaces the marker per row, and scopes the
depending node to the caller's grant for scoped tokens. The query is a
pure-correctness change with no measurable regression on the same input shape:
the marker is a bounded, correlated `EXISTS { MATCH (r)<-[:DEPENDS_ON]-(dep:Repository) ... }`
subquery anchored on the already-bound repository row. It is a single directed
relationship hop that short-circuits on the first inbound edge and never fans
out, so per-row cost is O(1) in inbound degree and the list query stays within
the bounded SKIP/LIMIT page. This `EXISTS { MATCH (x)<-[:TYPE]-(:Label) }` shape
is already exercised by the backend-conformance corpus
(`internal/backendconformance/corpus.go`), proving it is portable across NornicDB
and Neo4j on the pinned binaries. A live full-corpus `EXPLAIN`/wall-clock capture
was not run because this environment has no provisioned graph stack (stated per
cypher-query-rigor; matches the no-live-backend posture of #3380/#3389); to
finalize on a live stack, run the repositories list query and confirm the marker
subquery resolves via the `Repository` label anchor without an all-node or
relationship-fanout scan.

Observability Evidence: No-Observability-Change. The repository list/catalog
response shape, truth envelopes, `repository_*` timer stages, structured logs,
and HTTP status output are unchanged; the `is_dependency` field keeps its
boolean wire contract (OpenAPI `Repository.is_dependency`) and only its value
becomes truthful.

Deferred (follow-up issue, no IP): package-evidenced repo-to-repo dependency
edges. Joining admitted consumption correlations (consumer repo -> package) with
admitted publication/ownership correlations (package -> publisher repo) would
produce a package-sourced `DEPENDS_ON` edge distinct from the runtime-services
one. This is blocked today because publication/ownership correlations are held
`provenance_only=true, canonical_writes=0` until corroborating build/release/CI
evidence exists (`internal/reducer/package_publication_correlation.go`,
enforced by `package_source_correlation_test.go` and
`package_publication_correlation_test.go`), and the package canonical writer is
explicitly barred from adding Repository/ownership edges
(`internal/storage/cypher/README.md`). The follow-up must first admit publisher
truth, then emit the joined edge, then add a repo-scoped filter to
`GET /api/v0/dependencies` (currently package-native) so the console can deep-link
a repo into its package dependency chain.

## Service ingress posture (WAF/TLS) (#3403)

`enrichServiceQueryContext` adds an `ingress_posture` block to the service
context (`GET /api/v0/services/{name}/context`) so the entrypoint-first Exposure
Path console view can render WAF coverage and TLS termination tiles. The block is
derived strictly from the two materialized AWS protection edges
(`AWS_wafv2_web_acl_protects_resource`, `AWS_acm_certificate_used_by_resource`)
terminating on the service's own internet-facing edge resources, so the surface
never over-claims protection. `waf_coverage` and `tls_termination` are
three-valued (`protected`/`unprotected`/`unproven` and
`terminated`/`not_terminated`/`unproven`) so an observed-negative is never
confused with missing evidence.

Performance Evidence: No-Regression. The `ingress_posture` stage
(`loadServiceIngressPosture`, `service_ingress_posture.go`) runs only when the
already-loaded `cloud_resources` contain at least one internet-facing edge
resource (CloudFront/ALB/ELB/API Gateway), and does no work at all (no graph
round-trip) when the service has no edge resource. As of #5287 it runs three
bounded single-clause set queries (base edges / WAF-protected / ACM-terminated)
merged by membership in Go, replacing the prior single `OPTIONAL MATCH`
aggregation — which the pinned NornicDB build mis-executes (returns a null
edge_id and reports every edge as protected). The WAF/ACM lookups are bounded by
the (subset) protection-edge population, measured no-regression at 3,633 WAF
edges (1.2 ms, ≈ the base label scan); see
`docs/internal/evidence/5287-ingress-posture-nornicdb.md`. Before: the context returned no WAF/TLS posture. After: one bounded,
edge-id-anchored read derives the posture inside the existing few-seconds context
SLA. Baseline/after: derivation reuses the `WorkloadInstance-[:USES]->`
`CloudResource` set the context already resolves; no new whole-graph scan is
introduced. Pure-assembler and loader behavior are pinned by
`TestBuildIngressPosture*`, `TestEdgeResourcesFromCloudResources*`, and
`TestLoadServiceIngressPosture*` in `service_ingress_posture_test.go`
(`go test ./internal/query -run 'IngressPosture|EdgeResourcesFromCloudResources' -count=1`).

Observability Evidence: the stage emits a `startServiceQueryStage`
`ingress_posture` span with `waf_coverage`, `tls_termination`, and `edge_count`
attributes via the existing service-query stage timer, so an operator can see the
stage outcome and latency on the same span family that already diagnoses the
service context assembly.

## Package-evidenced repo-to-repo dependency chains (#3422)

Context: #3422 is the follow-up #3394 (PR #3421) deferred. PR #3421 made the
"Dependency repos" tile truthful from the inbound runtime-services
`(:Repository)-[:DEPENDS_ON]->(:Repository)` edge but explicitly deferred the
package-evidenced chain because the package publication/ownership correlations are
intentionally held `provenance_only=true, canonical_writes=0`
(`internal/reducer/package_publication_correlation.go`, enforced by
`package_publication_correlation_test.go`) and the package canonical writer is
barred from adding Repository/ownership edges
(`internal/storage/cypher/README.md`).

Truth design (architect-reviewed): resolve the chain entirely on the read side;
write no graph edge and change no reducer/writer. `ResolvePackageDependencyChains`
(`package_registry_dependency_chains.go`) joins, for one repository, its admitted
manifest-backed consumption correlations (consumer repo -> package,
`provenance_only=false`, `canonical_writes>=1`) with the provenance-only
publication/ownership correlations for each consumed package (package ->
publisher repo). The handler `GET /api/v0/package-registry/dependency-chains`
(`package_registry_dependency_chains_handler.go`,
capability `package_registry.dependency_chains.list`) surfaces each chain with the
canonical consumption leg and zero or more inferred publisher legs, each carrying
`provenance_only`/`canonical_writes` per leg. The console renders publisher legs
with the `inferred` truth chip, never `exact`. A materialized package-sourced
`DEPENDS_ON` edge was rejected because it would over-admit provenance-only truth
into the canonical graph, violating the writer bar and the provenance-only
invariant.

Why per-leg labels, not the envelope: `BuildTruthEnvelope` derives the envelope
level from the basis, and `TruthBasisSemanticFacts` collapses to `exact`
(`contract.go`), exactly as the sibling `/correlations` endpoint already ships
provenance-only rows under a semantic-facts envelope. The inferred/provenance-only
distinction therefore travels per leg in the response payload, not in the
envelope level.

Accuracy / proof matrix (`package_registry_dependency_chains_test.go`,
`package_registry_dependency_chains_handler_test.go`):

- Positive: consumer -> package -> single provenance-only publisher resolves; the
  consumption leg is canonical and the publisher leg is `provenance_only=true`.
- Negative: a consumed package with no publisher correlation keeps the chain and
  terminates at the package (the consumption row is never dropped, no publisher is
  synthesized).
- Ambiguous: more than one candidate publisher marks the chain `ambiguous=true`
  and surfaces every candidate; it never collapses to a single asserted
  publisher.
- Self-reference: a publisher repository equal to the consumer repository is
  dropped so a repo never appears to depend on itself through its own package.
- Out-of-scope publisher: the existing scope gate
  (`packageRegistryCorrelationFilterWithRepositoryAccess`,
  `$allowed_repository_ids`/`$allowed_scope_ids`) excludes publisher rows outside
  the caller's grant, so a scoped caller sees the package terminal rather than a
  leaked out-of-scope repo.

Performance / No-Regression Evidence: the resolution is two bounded reads, not
1+N. Phase 1 is one consumption read anchored on the consumer repository
(`RelationshipKind='consumption'`, `LIMIT`). Phase 2 is one batched
publication/ownership read keyed by the distinct consumed package set via a new
`PackageIDs []string` filter that adds
`fact.payload->>'package_id' = ANY($9::text[])` to
`listPackageRegistryCorrelationsQuery`. That predicate stays eligible for the
existing partial index `fact_records_package_correlations_v2_lookup_idx`
(`schema_fact_records.go`), which leads on `(payload->>'package_id')` over the
three package-correlation fact kinds with `is_tombstone = FALSE`, so the batched
`= ANY` resolves as an index scan bounded to the consumed package set rather than
a per-package round trip or a whole-`fact_records` scan. The package set is
bounded by the phase-1 `LIMIT` and phase 2 is itself capped at
`packageRegistryMaxLimit`. A live full-scale `EXPLAIN`/wall-clock capture was not
run because this environment has no provisioned ~500k Postgres stack (stated per
cypher-query-rigor; matches the no-live-backend posture of #3389/#3394); to
finalize on a live stack, run the dependency-chains route for a package-heavy
repository and confirm both reads resolve via
`fact_records_package_correlations_v2_lookup_idx` (Index Scan / Index Only Scan),
not a Seq Scan on `fact_records`. The query keeps the byte-for-byte semantics of
the existing scalar `package_id`/`repository_id` reads for all existing callers
(the new `$9` array is empty for them), proven by the unchanged
`internal/query`, `cmd/api`, `cmd/mcp-server`, `internal/mcp`,
`internal/storage/postgres` suites (`go test ... -count=1`, 4754 passed) and
`go vet` (no issues).

Observability Evidence: the route emits the new
`query.package_registry_dependency_chains` request span
(`telemetry.SpanQueryPackageRegistryDependencyChains`, registered in
`internal/telemetry/contract_package_registry.go` and pinned by the
`TestSpanNames` golden) with the standard `http.route` and `eshu.capability`
attributes, and reuses the existing `postgres.query` spans and
`eshu_dp_postgres_query_duration_seconds` for both bounded reads. No new
high-cardinality metric label, graph write, queue consumer, or reducer path is
added; the response carries `count`/`limit`/`truncated`/`next_cursor` plus the
per-leg `provenance_only`/`canonical_writes` so a slow or partial page is
diagnosable from the trace and payload alone.

## Impact findings list — winners read switch (#3389 Phase 2)

`GET /api/v0/supply-chain/impact/findings`
(`supply_chain_impact_findings_queries.go`, store gate in
`supply_chain_impact_findings.go`).

The legacy read deduplicates at query time
(`ROW_NUMBER() OVER (PARTITION BY canonical_key ...)`), sorting the full filtered
set and spilling ~98MB at a broad page (~1873ms at ~125k matches). With the
maintained `supply_chain_impact_canonical_winners` read model now live (Phase 1),
this adds `listSupplyChainImpactFindingsFromWinnersQuery`: the same page served
by filtering + keyset + LIMIT on the winners table alone (denormalized filter
columns), joining `fact_records` by `winner_fact_id` only for the page payloads.
No read-time dedup, no active-generation re-join (winner currency is
materialization-enforced).

Gate: `PostgresSupplyChainImpactFindingStore.ReadFromWinners`, resolved from the
`ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ` env at both the API and MCP wiring sites.
Default false (legacy read) — reversible, and only enabled after the reducer
maintainer has populated the winners table.

No-Regression Evidence: the winners read is byte-identical to the legacy read.
On the seeded 500k-impact-fact Postgres 18, the shipped winners query and the
legacy query were run via `COPY (...) TO STDOUT WITH (FORMAT csv)` with identical
parameters; `diff` is empty across 13 filter/sort/cursor combinations: no filter
(500000), impact_status (125000), severity_bucket (100000), ecosystem
case-insensitive (166666), repository_id (556), priority_bucket (125000),
min_priority_score (250000), detection_profile=precise — the complex precise
branch over observed_version + match_reason (250000), service_id membership
(500), priority_score_desc sort (500000), priority_score_desc + cursor (4999),
allowed-repository scoping `$22` (556), and — critically for cursor scoping — a
`repository_id` filter paired with a `priority_score_desc` cursor whose
`after_finding_id` belongs to a *different* repository (555): the out-of-filter
cursor row contributes no priority, exactly as legacy `canonical_facts` does.

The cursor priority lookup reads from a `WITH filtered AS NOT MATERIALIZED (…)`
CTE carrying the same filter + grant predicates as the page, so an out-of-grant
or out-of-filter `after_finding_id` cannot skip or surface authorized rows
(addresses the read-gate review: the lookup must not read the whole winners
table). `NOT MATERIALIZED` lets the planner inline the CTE so the page scan stays
index-served. Unit coverage:
`TestSupplyChainImpactReadGateSelectsQuery` pins gate→query selection,
`TestSupplyChainImpactWinnersReadQueryShape` pins the winners-table/no-dedup/
no-active-gen-rejoin shape plus the filtered-CTE cursor scoping, and
`TestSupplyChainImpactWinnersReadEnabled` pins the `strconv.ParseBool` env parse.

Performance Evidence (`EXPLAIN (ANALYZE, BUFFERS)`, `work_mem=16MB`, same seed):
- Common browse page (default `finding_id` sort, first page, `LIMIT 51`):
  **0.58 ms** — `Index Scan` on the winners `finding_idx` feeding 51
  `fact_records_pkey` lookups, no external-merge sort, no spill. Bounded O(page)
  regardless of corpus size, because the dedup happened off the read path in the
  maintainer. Filtered browses (e.g. `impact_status`) stay index-served too.
- `priority_score_desc` **cursor** page over the full 500k set: **~742 ms**
  (top-N over the filtered set; the COALESCE cursor predicate is not
  index-rangeable, the same shape limit the legacy read had) — vs legacy
  read-time dedup **1873 ms** at the same shape, ~2.5x faster with no spill. This
  is the one path that is O(filtered-scan) rather than O(page); the dominant
  browse/filter cases above are O(page).

No-Observability-Change: the route keeps its `query.supply_chain_impact_findings`
span, the `eshu_dp_postgres_query_duration_seconds` histogram, the readiness
envelope, truth envelope, and `count`/`limit`/`truncated`/`next_cursor`. The gate
adds one boolean env var; no metric, label, graph write, or queue consumer is
added. Operators can confirm which read path is active from the endpoint latency
and the winners table's `MAX(materialized_at)` recency. (Surfacing winners
freshness in the truth envelope is the immediate follow-up and is the prerequisite
for enabling the gate in production.)

## Impact findings list — winners freshness reporting (#3389 Phase 3)

`GET /api/v0/supply-chain/impact/findings`
(`supply_chain.go` handler, `supply_chain_impact_findings.go` store).

Phase 2 enables serving the list from the maintained winners read model, but
that read could lag the source facts behind the reducer maintainer's resweep
cadence. This phase makes the read self-report that lag in the response truth
envelope, so the gate can be turned on without ever presenting a cadence lag,
an unpopulated table, or a probe failure as fresh truth — the I4 invariant from
the canonical-dedup ADR (#3427) and the prerequisite the Phase 2 note named.

Mechanism: row presence in the winners table cannot be the freshness signal,
because an empty winners table is ambiguous — it is also the correct state after a
resweep with zero active findings (`RebuildAllWinners` deletes rows no longer in
the active set). Using row presence would pin a legitimate zero-findings corpus to
`building` forever. So the maintainer stamps a separate singleton watermark table,
`supply_chain_impact_winners_materialization`, in the SAME atomic rebuild
statement: the winners upsert and delete become data-modifying CTEs and the final,
unconditional statement upserts the watermark, so it is stamped even when the
resweep produced zero winners. The store probes `SELECT materialized_at FROM
supply_chain_impact_winners_materialization LIMIT 1` (one singleton row, no
aggregate). `applyWinnersFreshness` maps it:

- legacy live read (gate off) → fresh, untouched (and no probe is issued);
- watermark within `supplyChainImpactWinnersFreshnessWindow` (2m, several resweep
  cadences) → fresh, with the watermark surfaced as `freshness.observed_at` —
  regardless of how many winners the resweep produced, so a zero-findings corpus
  reads fresh-empty, not building;
- watermark older than the window → `stale` + cause `reducer_backlog` + bounded
  `next_check` (the maintainer is not keeping the model current);
- no watermark row at all (maintainer has never reswept) → `building` + cause
  `reducer_backlog`;
- probe error → `unavailable` (never silently fresh); the findings page still
  serves, the probe error is recorded on the span.

Accuracy Evidence: `TestApplyWinnersFreshness` pins all six mappings (legacy
untouched incl. on probe error, fresh+observed_at, stale+cause+next_check,
no-watermark→building+cause, unavailable); `TestSupplyChainImpactWinnersWatermark
Gate` pins that the legacy path issues no probe and never claims winners service
while the winners path issues exactly the watermark query and reports serving-
from-winners even when the probe errors; `TestRebuildSupplyChainImpactWinnersSQLIs
AtomicReconcile` pins the upsert + delete CTEs + unconditional watermark upsert in
one statement; `TestBootstrapDefinitionsIncludeSupplyChainImpactWinnersMaterializa
tion` and the bootstrap order/mirror tests pin the new table. Validated against
Postgres 18 with the shipped rebuild structure: a never-reswept DB (a stale winner
row, no watermark) → probe returns 0 rows (building); a resweep-to-zero-findings
(empty `winners_now`) → the stale winner is deleted (count 0) AND the watermark is
stamped → probe returns the watermark (fresh-empty, the exact case row-presence
would have mis-reported as building); the singleton `CHECK (singleton = 1)` keeps
exactly one watermark row across repeated resweeps.

Performance Evidence: the watermark probe reads a single-row singleton table
(`supply_chain_impact_winners_materialization` holds at most one row by the
`singleton` PK + `CHECK`), so `SELECT materialized_at ... LIMIT 1` is O(1) by
construction — independent of winners-table or corpus size, no aggregate, no
index needed. The legacy read issues no probe at all (the store short-circuits on
the gate before any query), so the freshness work is zero-cost until the read gate
is enabled and sub-millisecond once it is. The watermark upsert rides the existing
atomic resweep statement (one extra singleton upsert per ~30s resweep), so it adds
no extra round-trip or transaction to the maintainer. No change to the findings
read path itself.

Observability Evidence: the route adds `eshu.query.freshness_state`
(`fresh`/`stale`/`building`/`unavailable`, low cardinality) to its existing
`query.supply_chain_impact_findings` span, records the probe error on the span
when it fails, and the response `truth.freshness` now carries `state`,
`observed_at` (the resweep watermark), `cause`, and the bounded `next_check`
(`GET /api/v0/status`) so an operator can see a lagging or building read model
and its drilldown from the trace and the payload alone. No new env var, metric
series, graph write, or queue consumer is added.

## #3650 commit_sha in repository deployment-evidence read paths

`queryRepoDeploymentEvidence` now returns `artifact.commit_sha` from both the
outgoing and incoming `EvidenceArtifact` graph reads, and both read-model paths
(`copyOptionalDeploymentEvidenceFields` for the graph rows,
`deploymentEvidenceArtifactFromPreview` for the Postgres preview rows) copy the
version pin into the repository deployment-evidence response. The reducer already
`SET artifact.commit_sha` on the node; this closes the read side so the pin flows
end to end. The field is omitted when absent (no fabricated citation).

Accuracy Evidence: `go test ./internal/query -run
'TestGraphDeploymentEvidenceReturnsCommitSHA|TestDeploymentEvidenceArtifactFromPreviewCommitSHA|TestDeploymentEvidenceArtifactFromPreviewNoCommitSHADegradesSafely|TestContentReaderDeploymentEvidenceHydratesCommitSHA'
-count=1` pins the positive case (commit_sha surfaces on the artifact through the
graph RETURN, the Postgres preview path, and the read-model SQL→scan path) and
the negative/ambiguous case (commit_sha absent from details → field omitted, no
fabrication). The full `./internal/query` suite (5287 tests across the four gate
packages) confirms no confidence-semantics or sibling-field regression.

Performance Evidence / No-Regression Evidence: the change adds one already-indexed
node property (`artifact.commit_sha`) to the projection list of two existing
`EvidenceArtifact` reads bounded by `repositoryDeploymentEvidenceArtifactLimit`
(50 + 1). No new MATCH, traversal, hop, aggregate, sort, or round-trip is added —
the queries keep their artifact-first boundary shape and per-direction LIMIT, so
the cost is a constant per-row field read independent of corpus size. The
read-model SQL is unchanged (commit_sha already rides the `details` JSON payload).

Observability Evidence / No-Observability-Change: no new env var, metric series,
span, graph write, or queue consumer is added; the existing repository-context
query span and structured logs remain the diagnostic surface, and the new field
is additive on the existing `deployment_evidence` response object.
