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
Docker Compose, `~/bg-repos` corpus: 33 Repository, 21 Workload, 7 Platform, 92
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
local Compose backend (NornicDB, `nornic` database, `~/bg-repos` corpus,
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
