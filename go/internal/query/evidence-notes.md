# Query Evidence Notes

This file holds issue-specific no-regression, performance, and observability
notes for `internal/query`. Keep the package overview in `README.md`; put longer
evidence records here when they would otherwise push the overview past the
repository line budget.

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
