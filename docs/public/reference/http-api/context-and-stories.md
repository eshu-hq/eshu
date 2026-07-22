# HTTP Context And Story Routes

Use these routes when the caller has an exact name or canonical entity and needs
context, catalog navigation, a narrative story, or an investigation packet. The
route list is verified against `go/internal/query`.

## Route Map

| Area | Routes |
| --- | --- |
| Entity resolution | `POST /api/v0/entities/resolve` |
| Incident context | `GET /api/v0/incidents/{incident_id}/context` |
| Work-item evidence | `GET /api/v0/work-items/evidence` |
| Context | `GET /api/v0/entities/{entity_id}/context`, `GET /api/v0/workloads/{workload_id}/context`, `GET /api/v0/services/{service_name}/context`, `GET /api/v0/repositories/{repo_id}/context` |
| Catalog | `GET /api/v0/catalog` |
| Stories | `GET /api/v0/repositories/{repo_id}/story`, `GET /api/v0/workloads/{workload_id}/story`, `GET /api/v0/services/{service_name}/story`, `POST /api/v0/impact/trace-deployment-chain`, `POST /api/v0/impact/deployment-config-influence` |
| Intelligence report | `GET /api/v0/services/{service_name}/intelligence-report` |
| Investigation | `GET /api/v0/investigations/services/{service_name}` |

OpenAPI remains canonical for full request and response schemas.

Programmatic clients that need route-to-route comparison should request the
canonical envelope with `Accept: application/eshu.envelope+json`. Repository,
entity, workload, and service context/story routes then return `data`, `truth`,
and `error` at the top level. Plain HTTP clients that do not request the
envelope keep the legacy route payload shape.

## Entity Resolution

`POST /api/v0/entities/resolve` accepts `name`, optional `type`, optional
`repo_id`, and optional `limit`. `name` is required. The response includes
`entities`, `count`, normalized `limit`, and `truncated`.

Name matching is exact and case-sensitive. When `repo_id` is omitted, `type`
is required and must identify a content-backed entity family; unknown types
fail closed. Global `repository`, `directory`, and `file` resolution requires
`repo_id` because those graph-only families cannot be represented completely
by the content snapshot. Canonical `content-entity:` IDs and `type=workload`
retain their dedicated exact content and authoritative graph paths.

Use this route before context or story routes when the caller has an exact
entity name or canonical content handle and needs its stable identifier.

## Context

Context routes are canonical-ID oriented:

- entity context requires `entity_id`
- workload context requires `workload_id`
- repository context requires `repo_id`
- service context is an alias over workload context and adds
  `requested_as=service`

When a repository has workload identity facts but no materialized `Workload`
node, service context can fall back to the repository read model. Those
responses use `materialization_status=identity_only`,
`query_basis=repository_read_model`, an empty `instances` array, and a
`limitations` entry of `workload_identity_not_materialized`.

Entity context may include semantic narrative fields when normalized semantic
metadata exists: `semantic_summary`, `semantic_profile`, and `story`.

Entity context, workload context, and workload story responses are prompt-ready:
alongside the canonical truth envelope they carry two additive fields so a caller
sees bounds and missing evidence without falling back to raw Cypher.

- `result_limits` is a drilldown block with a bounded `limit`, deterministic
  `ordering`, fan-out counts (`relationship_count` for entity context;
  `instance_count`, `dependent_count`, and `consumer_count` for workload
  context/story), a `truncated` flag, the `drilldown_tool` to call next
  (`get_relationship_evidence` for entity context, `get_workload_story` from
  workload context, `get_workload_context` from workload story), the
  `drilldown_basis`, and the `context_path` for re-reading the route. The entity
  and workload relationship fan-out is capped in place so the prompt-ready read
  stays within the route budget and exposes truncation explicitly.
- `partial_reasons` is always present (possibly empty) and promotes the context
  payload's `limitations` into an explicit, sorted, de-duplicated array so the
  envelope shape is stable across complete and partial reads.

Entity context additionally reports incomplete relationship truth with
`relationships_complete=false` and a machine-readable
`relationships_truncation_reason`:

- `k8s_resource_candidate_scan_truncated_at_5000` means the bounded K8s
  `SELECTS` candidate scan reached its repository ceiling.
- `github_actions_source_cache_truncated` means a GitHub Actions workflow's
  32 KiB `source_cache` cap prevented complete dependency extraction.

The GitHub Actions source-cache condition also appears in `partial_reasons`.
The K8s candidate-scan condition is instead disclosed through
`relationships_complete=false` and `relationships_truncation_reason`; it is not
added to `partial_reasons`. Both conditions set `result_limits.truncated=true`,
so clients must not treat the returned relationship list as complete.

### Deployment Trace Relationship Endpoints

`POST /api/v0/impact/trace-deployment-chain` keeps deployment-source
relationship families distinct. Every `deployment_sources[]` row includes
`relationship_type`, canonical `source_id`, and canonical `target_id`:

- `DEPLOYMENT_SOURCE` is `WorkloadInstance -> Repository` runtime admission
  evidence.
- `DEPLOYS_FROM` is `Repository -> Repository` deployment configuration
  evidence.

`deployment_fact_summary.deployment_truth_tier` classifies the strongest
deployment evidence available for the traced workload using the closed
[deployment truth tier vocabulary](../deployment-truth-tiers.md):
`runtime_confirmed`, `provenance_ci_declared`, `declared_ref`, or
`config_only`. The tier is additive; existing `overall_confidence` and
`overall_confidence_reason` fields are unchanged.

Consumers must render the returned direction and endpoints. A deployment-source
repository name is display text, not permission to convert an instance edge into
a repository edge. Deployment-source expansion is capped at 50 rows. The
`deployment_source_limits` object reports the returned and observed counts,
per-family observed counts, deterministic ordering, truncation, and whether the
observed count is only a lower bound because a graph query reached its sentinel.

Impact graph consumers may report `complete within bounds` only when four
independent metadata families are present and internally consistent:
`runtime_topology_limits`, `deployment_source_limits`,
`cloud_resource_limits`, and `k8s_resource_limits`. The runtime block contains
separate bounds for instances, direct platform edges, and provisioned
platforms. The Kubernetes block contains separate content and
deployment-source probes because those inputs are merged and deduplicated
before the public cap. Missing, malformed, or contradictory metadata means
`completeness unverified`; it never proves a complete empty collection.

`cloud_resource_limits` describes only canonical resources returned from a
materialized workload-instance `USES` relationship. It separately reports the
resource bound and the pre-aggregation relationship-observation bound; a true
`observation_count_is_lower_bound` means an observation or resource sentinel
prevented an exact global observation count. Observation-only truncation does
not prove that a whole resource identity was omitted, so consumers must not
invent an omitted-resource count from that signal. When the handler reuses
older context rows that were not sentinel-probed, it omits this block and the
consumer must fail completeness closed.

`WorkloadInstance`, `INSTANCE_OF`, `RUNS_ON`, and `USES` do not currently carry
canonical repository ownership. Repository-scoped callers therefore receive no
runtime-instance, direct-platform, or materialized cloud-resource evidence from
those global relationships. This fails closed instead of treating a selected
repository's `DEFINES` edge as ownership of every shared workload observation.
All-scope and shared sessions continue to receive this topology.

`controller_overview.entity_limits`
separately bounds service-matched controller entities and discloses source-scan
saturation. `image_refs` contains images from returned bounded Kubernetes rows
only; images belonging solely to omitted rows are not returned.

The top-level `topology_edges[]` array carries the selected subject backbone:
`DEFINES` from `repo_id` to `workload_id`, plus `INSTANCE_OF` from every
returned `instance_id` to that workload. Consumers should treat a missing or
mismatched backbone edge as incomplete topology rather than reconnecting rows
by name.

`instances[].platforms[].platform_id` is the canonical platform identity;
`platform_name` is only its label. Instance platforms carry
`topology_basis=direct_runtime` and exact `RUNS_ON` topology edges from their
containing instance to that platform.

Repository-level provisioning is returned separately in
`provisioned_platforms[]`, where `topology_basis=provisioning_fallback` preserves
two relationship families:

- `PROVISIONS_DEPENDENCY_FOR` from the infrastructure repository to the
  service repository; and
- `PROVISIONS_PLATFORM` from the infrastructure repository to the platform.

Consumers must not copy a provisioned platform beneath every instance or turn
repository provisioning into `RUNS_ON`. The instance
`environment` field is structured runtime evidence, not proof of a graph edge
to an `Environment` node.

Repository context relationship rows expose the same correlation confidence
metadata as relationship evidence drilldown: `confidence`, `confidence_basis`,
`resolution_source`, `evidence_type`, and `evidence_kinds` when the reducer or
graph edge has that data. `relationships` includes outgoing rows; the
`relationship_overview.relationships` section includes both incoming and
outgoing rows plus the same compact evidence pointers.

### Tech Fingerprint Rollup

Repository context (`GET /api/v0/repositories/{repo_id}/context`) and service
context (`GET /api/v0/services/{service_name}/context`) include two additive
tech-fingerprint fields when data is available:

- **`language_breakdown`** — a `{language: file_count}` map derived from
  indexed `File` nodes in the repository. It collapses the existing `languages`
  array into a compact rollup for dashboards and rollup queries. Omitted when no
  language data exists for the repository.
- **`source_tool_breakdown`** — a `{source_tool: edge_count}` map counting
  outgoing relationship edges from the repository that carry a `source_tool`
  property. The canonical `source_tool` vocabulary is defined in
  `go/internal/sourcetool` (terraform, helm, ansible, etc.). Omitted when no
  edges carry `source_tool`.

Both fields are read-only aggregates — no new capture, parser, or migration.
The underlying queries are bounded and anchored on the repository node's `id`
index (`MATCH (r:Repository {id: $repo_id})-[rel]->() WHERE rel.source_tool IS
NOT NULL`), satisfying the cypher-performance.md bounded-read contract.

For service context the breakdown is anchored on the service's primary
`repo_id` resolved from the workload context. When `repo_id` is absent
(identity-only path) both fields are omitted.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'TestGet(WorkloadContext|WorkloadStory|EntityContext)ReturnsEnvelopeWhenRequested' -count=1
cd go && go test ./internal/query -run 'TestGet(WorkloadContext|WorkloadStory|EntityContext)ReturnsResultLimitsAndPartialReasons' -count=1
cd go && go test ./internal/mcp -run 'TestDispatchTool(WorkloadContext|WorkloadStory|EntityContext)ReturnsHardenedEnvelope' -count=1
```

This proves entity context, workload context, and workload story responses honor
the same envelope negotiation used by repository and service story routes
through both HTTP and MCP, and that the additive `result_limits` and
`partial_reasons` fields are present, without changing their graph/content
lookup shape.

No-Observability-Change: context/story envelope normalization only changes the
HTTP response writer selected after the existing query and enrichment paths
finish. It adds no graph query, collector call, queue worker, metric instrument,
span attribute, or deployment knob.

## Incident Context

`GET /api/v0/incidents/{incident_id}/context` returns a bounded incident
packet from collected source facts. `provider` defaults to `pagerduty`;
`scope_id` disambiguates duplicate provider incident IDs; `service_id`,
`since`, and `until` bound fallback change candidates.

The response always includes an ordered evidence path for incident, service,
intended PagerDuty routing, applied PagerDuty routing, live PagerDuty routing,
deployable, runtime artifact, image, build/deploy record, commit, pull request,
and work item slots. Missing Jira, pull-request, runtime, image, build,
deployable, routing, or commit evidence is reported explicitly instead of
omitted.

Routing slots preserve source class. `intended_routing` comes from
Terraform-source `PagerDutyDeclaration` content rows. `applied_routing` comes
from active Terraform-state `incident_routing.applied_pagerduty_resource`
facts. `live_routing` comes from optional live
`incident_routing.observed_pagerduty_service` facts or scoped
`incident_routing.coverage_warning` gaps such as permission-hidden provider
state. These slots explain whether the incident service is declared, applied,
or currently visible in PagerDuty; they do not prove root cause, service
health, blast radius, deployable identity, image identity, commit, pull request,
or Jira work-item truth.

When a service-catalog operational link exactly names the PagerDuty service
URL, the read model can use reducer-owned catalog, container-image, and
Kubernetes correlation facts to fill deployable, image, and runtime artifact
slots. When CI/CD run correlation evidence names the selected image digest,
build/deploy and commit slots can be exact; tag-only image-reference matches
remain derived unless a later reducer fact proves an immutable artifact digest.
When a GitHub merged-pull-request trigger names the selected commit, the pull
request slot is exact provider evidence. Jira remote links to that
provider-verified PR, direct PagerDuty incident links, or issue keys in the PR
title can enrich the work-item slot, but Jira-only PR URLs do not verify
pull-request identity. Fallback change candidates are labeled separately from
exact provider evidence and from derived reducer edges, and name-only service or
tag matches are not promoted.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'TestPostgresIncidentContextStoreReadsCollectedPagerDutyIncidentBySourceRecordID|TestPostgresIncidentContextStoreReturnsAmbiguousSourceRecordMatches|TestIncidentContext(ChangeCandidateQueryCastsServiceIDParameter|ChangeCandidateQueryCastsNullableTimeParametersEverywhere|QueriesStayBoundedToActiveFacts|HandlerUsesBoundedStore|HandlerReturnsAmbiguousCandidates|HandlerRequiresIncidentIDAndLimit|RuntimeQueriesStayBoundedToExplicitEvidence)' -count=1
cd go && go test ./internal/mcp -run 'TestResolveRouteMapsIncidentContextToBoundedQuery|TestDispatchToolIncidentContextReturnsStructuredEnvelopeData' -count=1
cd go && go test ./internal/storage/postgres -run TestFactRecordSchemaIncludesIncidentContextSourceRecordFallbackIndex -count=1
```

This proves incident context reads keep bounded active-fact queries, resolve
collected PagerDuty `incident.record` facts by `source_record_id` when legacy
payloads omit `provider_incident_id`, keep the MCP tool response aligned with
the API envelope, and preserve a partial Postgres index for the source-record
fallback path.

No-Observability-Change: the route still runs under `query.incident_context`
with stable `http.route` and `eshu.capability` span attributes, existing
Postgres query instrumentation, envelope error reporting, and explicit
missing-evidence slots. No graph write, collector call, queue worker, metric
instrument, or deployment knob changes.

## Work-Item Evidence

`GET /api/v0/work-items/evidence` lists active Jira/work-item source facts.
Requests must include `limit` and at least one scope anchor: `scope_id`,
`project_key`, `work_item_key`, `provider_work_item_id`, `external_url`,
`url_fingerprint`, or `observed_after`. The route returns redacted evidence
rows, `missing_evidence`, state summaries, and `next_cursor.after_fact_id` when
the page is truncated.

The route is ticket-first evidence, not an incident or deployment verifier.
External URLs are converted to sanitized fingerprints and raw URLs are not
returned. Jira facts can show exact provider evidence, unsupported link types,
stale evidence, permission-hidden evidence, missing evidence, rejected unsafe
payloads, or metadata-collection warnings, but PR, commit, deployment, runtime
artifact, image, service, and incident truth require provider or reducer
evidence outside Jira.

A `work_item.metadata_warning` fact reports the `metadata_warning` evidence
state — metadata collection for a scope was blocked (archived, unsupported, or
permission-hidden), which is distinct from a hidden issue record. Its row also
carries `metadata_type`, `warning_reason`, and `provider_id_fingerprint`; the
evidence state stays `metadata_warning` regardless of the reason, and the
specific reason is in `warning_reason`.

A confidently typed GitHub pull-request or GitLab merge-request external link
also returns `linked_repository_id`, the canonical repository id the Jira
collector resolves from the link URL before redaction. It is the same
generation-independent id Eshu stores for every repository and carries no raw
URL, query parameter, credential, or user identity; un-canonicalizable or
ambiguous links omit it.

Scoped tokens authorize this route on `linked_repository_id`. A work item is
visible to a scoped token only when its durable `linked_repository_id` is within
the token's granted repository set; a multi-repo work item is visible for the
granted subset only. Work-item facts with no durable repository link — every
fact kind except a canonicalized external link, or a `scope_id`/`project_key`/
`work_item_key` selector that never resolved a repository — stay invisible to
scoped tokens (fail-closed), never surfaced as provider-scope rows. An empty
grant returns the bounded zero-evidence page without a store read. Shared,
admin, and local callers are unchanged and read the full work-item corpus.

## Catalog

`GET /api/v0/catalog` is the bounded navigation surface for Console and MCP
clients. It returns repository, workload, and service handles plus counts,
`limit`, `truncated`, and limitations when the runtime can only return
repository handles.

Each workload and service handle carries an `environments` array resolved from
graph evidence: `WorkloadInstance.environment` for materialized instances and
the `Environment` nodes reached through the defining repository's deployment
evidence (`(repo)-[:DEFINES]->(workload)` joined with
`(repo)<-[:EVIDENCES_REPOSITORY_RELATIONSHIP]-(:EvidenceArtifact)-[:TARGETS_ENVIRONMENT]->(:Environment)`).
A handle with no environment edge returns an empty array; environments are never
inferred from repository or workload names.

The optional `limit` caps each returned collection. The default is 2000 and the
maximum accepted value is 5000.

## Service Intelligence Report

`GET /api/v0/services/{service_name}/intelligence-report` composes the service
story into an operator-ready [service intelligence report](../service-intelligence-report.md)
(schema `service_intelligence_report.v1`): identity, code-to-runtime trace,
deployment/configuration, supply-chain, and incident/support sections, each
preserving the source truth label and evidence handles, plus deterministic
suggested investigations. It runs no LLM path. The live route sources
`supply_chain` from reducer-owned supply-chain impact inventory and
`incidents_support` from durable incident-routing evidence; either section stays
`unsupported` with its fallback next call when its evidence is empty or the load
fails. It accepts the same `service_id`, `repo`, and `environment` selectors as
the service story route and returns the same capability (501), not-found (404),
and ambiguity (409) contracts. The MCP tool
`get_service_intelligence_report` dispatches to this route, so API and MCP
return the same report.

## Stories

Story routes return structured narrative first and drilldown handles second.
They are the right entry point for onboarding, support, service explanation,
and documentation generation prompts.

`GET /api/v0/workloads/{workload_id}/story`, `GET /api/v0/services/{service_name}/story`,
`GET /api/v0/repositories/{repo_id}/story`, and `POST /api/v0/impact/trace-deployment-chain`
may each include `evidence_boundaries`: a static, closed-vocabulary array of
`{domain, read_surface, reason}` objects disclosing Postgres-only reducer
domains that route's graph-sourced sections omit (see the
graph-projection-policy design doc). The field is present only when a
boundary applies to that route and is absent (not an empty array) otherwise; a
domain already served by a sibling top-level response field is never listed as
a boundary for that route, since there is no omission to disclose. Service
story never emits `evidence_boundaries` today: its `ci_cd_evidence` field
already serves ci_cd_run_correlation, and `code_to_runtime_trace`'s
`image_package` segment already serves container_image_identity, so both
candidate domains are fully covered and `evidence_boundaries` is absent from
every service story response. `evidence_graph` alone omits ci_cd/supply-chain
graph edges (no BUILT_FROM edge is projected yet for either domain), but that
narrower sub-surface gap is not disclosed as a whole-route boundary.

Service story `evidence_graph.nodes[]` assigns source-backed roles for the
workload anchor, source repository, deployment configuration, runtime instance,
and downstream consumer. Repository nodes may also carry privacy-safe
`canonical_key` and `scope_key` fields. `RUNS_AS` edges are emitted only for
instances present in the selected workload evidence; the route does not infer
ECS, EKS, or other runtime multiplicity from labels. Node and edge collections
remain deterministically ordered and bounded, with `edge_count` and `truncated`
reporting any source-side clipping before visualization derivation.

Service story and service context classify hostname-shaped content evidence
before returning entrypoints. Exact hostnames are returned in `hostnames` and
may become public hostname `entrypoints`. Documented docs/spec routes remain
internal `docs_route` entrypoints. Dotted config keys, fixture field paths, and
two-label ambiguous candidates are returned only in `entrypoint_candidates`
with `classification` and `reason`; they are supporting evidence, not public
hostname entrypoints.

No-Regression Evidence:

```bash
cd go && go test ./internal/contentrefs -run 'TestHostnamesRejectsDottedConfigKeysAndFieldPaths|TestHostnameCandidatesClassifyRejectedAndAmbiguousEvidence' -count=1
cd go && go test ./internal/query -run 'TestLoadServiceQueryEvidenceClassifiesNonEntrypointHostnameCandidates|TestBuildServiceStoryResponseExposesNonEntrypointCandidates|TestRepositoryStoryReadbackKeepsDocsRoutesWithoutHostnameEntrypoints' -count=1
```

No-Observability-Change: this is hostname candidate classification and response
shaping over content the service context/story route already loads. Operators
continue to diagnose the path through existing service query stage timing logs,
`service_evidence_content` hostname and environment counts, content-store query
instrumentation, `platform_impact.context_overview` truth envelopes, and
HTTP/MCP envelope errors. No graph write, queue domain, worker, metric
instrument, span name, route, runtime flag, or pprof behavior changes.

Service story supports disambiguation with:

- `service_id` for an exact workload/service ID
- `repo` for repository-scoped disambiguation
- `environment` for environment-scoped disambiguation

When a service name matches multiple workloads, service story returns HTTP 409
with envelope `error.code=ambiguous`, `data=null`, and candidate details. It
does not choose the first match.

Service and repository story `documentation_overview` may include
`target_documentation` when the documentation read model has admissible
external documentation tied to the selected story target. The nested object
uses the same bounded readback vocabulary as documentation target routes:
`findings`, `finding_count`, `related_facts`, `related_fact_count`,
`coverage`, `missing_evidence`, `limit`, and `source`. Service story reads the
selected service target, including canonical `service_id` selectors forwarded
by MCP. Repository story reads repository-target documentation. Generic text
mentions are not enough for admission; the documentation fact or finding must
carry target references such as `candidate_refs`, `evidence_refs`, or
`linked_entities`. When target-related facts exist but no admissible finding is
linked to the target, the story preserves explicit `missing_evidence` instead
of silently presenting an empty documentation summary. When external
documentation source facts exist but none carry structured target refs, stories
and `GET /api/v0/documentation/findings` keep `findings`, `finding_count`,
`related_facts`, and `related_fact_count` at zero and report
`target_link_not_modeled` with aggregate `coverage.source_only_count` and
`coverage.source_only_fact_kinds`; `GET /api/v0/documentation/facts` remains
target-scoped and does not return source-only Confluence rows for the target.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'Test(DocumentationHandlerExplainsSourceOnlyDocumentationFacts|ContentReaderDocumentationFindingsReportsSourceOnlyDocumentationFacts|BuildStoryTargetDocumentationExplainsSourceOnlyDocumentationFacts|BuildDocumentationSourceOnlySQLStaysAggregateOnly|GetServiceStorySurfacesTargetLinkedExternalDocumentation|GetRepositoryStorySurfacesTargetLinkedExternalDocumentation|GetServiceStoryPreservesMissingExternalDocumentationCorrelation|DocumentationPayloadDoesNotMatchGenericMentionWithoutTargetRef)' -count=1
cd go && go test ./internal/mcp -run 'TestDispatchToolServiceStoryPreserves(SourceOnlyDocumentationReadback|MissingDocumentationReadback|TargetDocumentationReadback)' -count=1
```

Observability Evidence: service story records the target-documentation read
inside the existing `service_query.stage_completed` event for
`documentation_overview` with `has_target_documentation`,
`target_documentation_finding_count`, and `error` attributes. Repository story
emits a bounded `repository_query.stage_completed` event for the
`target_documentation` stage with `has_result`, `finding_count`, and `error`.
The read model uses existing Postgres spans for `list_documentation_findings`
and `list_documentation_target_facts`, plus the aggregate-only
`count_documentation_source_only_facts` span and the same HTTP/MCP truth
envelope and error reporting. No reducer queue, graph write, collector,
worker, metric label, runtime knob, or deployment setting changes.

Service and repository story `support_overview` may include `target_support`
when Jira/work-item or PagerDuty incident-routing source facts carry explicit
target references for the selected service or repository. The nested object
contains bounded `evidence`, `evidence_count`, `work_item_count`,
`incident_routing_count`, `ambiguous_evidence`, `ambiguous_count`, `coverage`,
`missing_evidence`, `limit`, and `source`. Global collector rows are not target
truth by themselves: title text, service names, summaries, and generic mentions
do not attach support evidence. Facts must carry `candidate_refs`,
`evidence_refs`, or `linked_entities`. If a fact references the selected target
and another target, the story reports `support_correlation_ambiguous` instead
of admitting it as exact target support. If no target support facts are present,
the story reports `support_target_facts_absent`. If active Jira or PagerDuty
source facts exist but none carry structured refs for the selected target, the
story keeps `evidence_count`, `work_item_count`, and `incident_routing_count`
at zero and reports `support_source_only_not_target_linked` with aggregate
`coverage.source_only_count`, `coverage.work_item_source_only_count`, and
`coverage.incident_routing_source_only_count`.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'Test(GetServiceStorySurfacesTargetLinkedSupportEvidence|GetServiceStoryPreservesMissingSupportCorrelation|GetRepositoryStorySurfacesTargetLinkedSupportEvidence|BuildStoryTargetSupport|BuildServiceStoryTargetSupportSQL|BuildRepositoryStoryTargetSupportSQL|ContentReaderServiceStoryTargetSupportReportsSourceOnlySupportFacts)' -count=1
cd go && go test ./internal/mcp -run 'TestDispatchTool(ServiceStoryPreservesTargetSupportReadback|RepoStoryPreservesTargetSupportReadback)' -count=1
```

Observability Evidence: service story records support readback in
`service_query.stage_completed` with stage `support_target_evidence`,
`has_result`, `target_support_evidence_count`, and `error`. Repository story
emits `repository_query.stage_completed` for `target_support` with
`has_result`, `evidence_count`, and `error`. The Postgres read model uses the
existing `postgres.query` span family with operation
`list_service_story_target_support` against active `fact_records`; the
source-only fallback is an aggregate count over the same active support fact
kinds and does not return row payloads. No collector, reducer queue, graph
write, metric instrument, runtime flag, or deployment setting changes.

Deployment trace responses are evidence-first and may include deployment,
GitOps, controller, runtime, cloud, Kubernetes, image, relationship, and
fact-summary sections. Mapping modes are:

- `controller`
- `iac`
- `evidence_only`
- `none`

Service story and deployment trace keep canonical `cloud_resources` separate
from `uncorrelated_cloud_resources`. `cloud_resources` requires a materialized
workload-to-cloud relationship owned by the reducer. Exact service anchors and
explicit `READS_CONFIG_FROM` deployment evidence stay candidate or
missing-evidence inputs until that reducer-owned `USES` edge exists. Plain
service-name substrings are not enough to promote a cloud dependency.
`uncorrelated_cloud_resources` is a bounded candidate list for cloud resources
whose safe identity or anchor handles match the service, including `name`,
`id`, `kind`, `resource_type`, `resource_id`, `arn`, `service_kind`,
`account_id`, `region`, `source`, `source_system`, `config_path`, or
`service_anchor_name_tokens`. These rows still lack the workload-to-cloud
relationship, exact service anchor, or exact config-read evidence; callers
should treat them as missing evidence to investigate, not as attached
dependencies.
Deployment-config `READS_CONFIG_FROM` matches use the same candidate bucket:
they can explain why a resource should be investigated, but they do not create
the reducer-owned workload-to-cloud relationship. All candidate rows expose
`candidate_status` and `missing_relationship`. Deployment-config candidates
also expose `match_basis`; free-text candidates instead preserve their service-
anchor status, so `candidate_status` can be `uncorrelated`, `ambiguous_anchor`,
`stale_anchor`, or `weak_anchor`.
`uncorrelated_cloud_resources_truncated=true` reports that candidate discovery
was incomplete because the returned list was capped or deployment-config
evidence or anchor input was truncated. Additional candidates may therefore
exist even when no candidate rows were returned. Deployment-config candidates
are globally ordered by resource name and canonical ID before the response
bound is applied; deployment-evidence artifact order does not decide which
config-derived candidates survive that bound. Free-text candidate selection is
a separate query path and does not use deployment-evidence artifact order.

Repository story uses the same repository deployment-evidence read path as
repository context and service story. When repository-scoped deployment evidence
exists, repository story may populate deployment overview evidence counts,
tool families, environments, relationship types, and delivery paths even when a
materialized workload node is not available. In that case
`deployment_surface_unknown` must not be emitted, but `workload_surface_unknown`
can remain until workload materialization catches up.

Repository and service story responses also include `ci_cd_evidence` when a
repository scope is known. This block mirrors
`GET /api/v0/ci-cd/run-correlations` by keeping static workflow files,
provider run rows, and run-to-artifact/image bridges separate. Service stories
reuse the same block in the `code_to_runtime_trace` `ci_cd` segment, so missing
provider runs, ambiguous artifacts, and digest/image evidence use the same
reason classes across the CI/CD endpoint, repository story, service story, and
MCP transport.

No-Regression Evidence: `go test ./internal/query -run 'TestLoadRepositoryScopedCICDEvidenceUsesBoundedRepositoryScope|TestBuild(Repository|Service)StoryResponsePreservesCICDEvidenceSummary' -count=1` fails if repository or service stories stop using a bounded repository-scoped CI/CD readback or stop preserving the CI/CD evidence classes returned by that readback.

Observability Evidence: repository and service story CI/CD readback uses one
repository-anchored reducer fact read with `limit+1` truncation probing plus the
existing repository-scoped content file lookup for workflow files. It emits the
same stage-completed log shape for `repository_story/ci_cd_evidence` and
`service_story/ci_cd_evidence` with `has_result` and `error`; no graph traversal, broad
graph scan, graph write, queue, worker, metric instrument, metric label, or
runtime knob is added.

No-Regression Evidence: issue #1461 reproduced on current `main` with a
repository story fixture containing one repository-scoped deployment evidence
artifact and no materialized workload. The failing baseline returned
`deployment_surface_unknown`; after the fix, this command returns one deployment
evidence row, clears only `deployment_surface_unknown`, and leaves the workload
limitation intact.

```bash
go test ./internal/query -run 'Test(GetRepositoryStoryUsesReadModelDeploymentEvidence|BuildRepositoryStoryResponseSummarizesRepositoryOnlyDeploymentEvidence|BuildRepositoryStoryResponseDoesNotMarkDeploymentUnknownWhenWorkloadHasDeliveryEvidence)' -count=1 -timeout=60s
```

The broader read-path proof ran:

```bash
go test ./internal/query -run 'Test(GetRepository(Context|Story).*Deployment|QueryRepoDeploymentEvidence|QueryServiceDeploymentEvidence|BuildRepositoryStoryResponse.*Deployment|BuildServiceStoryResponse.*Deployment|GetServiceStory.*|GetWorkloadStory.*|BuildWorkloadStory.*)' -count=1 -timeout=120s
go test ./cmd/api ./internal/query ./internal/mcp -count=1 -timeout=180s
```

The proof backend is the query package in-memory `ContentStore`/`GraphQuery`
harness, exercising the same
NornicDB-compatible `GraphQuery` boundary and the content read model before
graph fallback. No reducer queue, graph write, or worker row is involved; the
terminal row count is one deployment evidence artifact read for the repository.

Observability Evidence: repository story now emits a bounded
`repository_query.stage_completed` event for the `repository_story` /
`deployment_evidence` stage with `has_result` and `error` attributes. Existing
route envelope truth metadata, HTTP status behavior, graph/content timing
instrumentation, and MCP envelope dispatch stay unchanged. No metric label,
collector, queue worker, runtime knob, or deployment setting changed.

`support_overview.spec_count` uses the same bounded API-surface evidence as
`api_surface.spec_count`. When graph-backed API evidence has spec paths but no
precomputed scalar count, story synthesis derives the count from those paths
instead of reporting zero in support overview or in the human narrative string.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'Test(GetServiceStoryReadbackAlignsSupportOverviewSpecCountWithAPISurface|ServiceStorySupportOverviewUsesAPISurfaceSpecPathCount|BuildServiceStoryResponseNormalizesAPISurfaceOnce)' -count=1 -race
cd go && go test ./internal/query ./internal/mcp -count=1
cd go && go test ./internal/mcp -run TestDispatchToolServiceStoryPreservesSpecCountConsistency -count=1
```

No-Observability-Change: service story spec-count alignment reuses the
already-loaded bounded `api_surface` map during response assembly. It adds no
new graph, Postgres, MCP dispatch, queue, collector, or runtime call; the
existing `service_query.stage_started` and `service_query.stage_completed`
events still cover the `graph_api_surface` and `overview_assembly` stages.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'TestTraceDeploymentChainKeepsConfigDerivedCloudResourcesAsUncorrelatedCandidates|TestConfigDerivedCloudResourceDependenciesRequireConfigReadEvidence|TestBuildDeploymentTraceResponseExplainsUncorrelatedCloudCandidates' -count=1
```

This proves deployment trace keeps canonical `cloud_resources` limited to
materialized workload-instance `USES` relationships. Explicit
`READS_CONFIG_FROM` matches remain bounded `uncorrelated_cloud_resources`
candidates with their config-evidence basis and missing relationship disclosed.

No-Observability-Change: service story anchor admission uses existing service
query stage timing and graph query instrumentation; it adds no collector call,
queue worker, metric label, runtime knob, or deployment behavior.

Service story `code_to_runtime_trace.image_package` attaches supply-chain
evidence only when a target deployment image reference resolves to an exact
container image identity and an admissible SBOM attachment. Ambiguous tags,
stale identity rows, missing image identities, and unattached SBOM rows stay
fail-closed as `missing_evidence` reasons so aggregate supply-chain evidence is
not promoted into a target service story by accident. When one target image has
valid identity and SBOM evidence but another target image is missing evidence,
the valid evidence remains in the trace and the missing reason stays explicit.
Identity and SBOM read-model pages probe one row past the public cap and treat
over-limit pages as ambiguous rather than admitting a partial page.
Deployment config evidence such as Helm values may supply a candidate image
reference through a generic matched value. The story accepts tagged or digested
container image refs, and it can also carry registry-qualified image repository
values from Helm config as candidates. Config paths, local build contexts, and
repository aliases remain non-image evidence. Candidate image references can
move the missing hop from `deployment_image_reference_missing` to a specific
candidate missing reason, but repository-only candidates do not create tag,
digest, SBOM, or vulnerability impact truth by themselves.

`image_package.missing_evidence_details[]` gives operators the bounded reason
for each candidate without inventing image identity. Repository-only values use
`deployment_image_reference_repo_only` and ask for a tag or digest. Tagged or
digested candidates whose normalized OCI repository id is absent from configured
OCI registry scope/work-item evidence use `oci_registry_target_outside_scope`
and name the `candidate_repository_id` to configure. Configured but failed
collector targets use `oci_registry_target_unreadable` with the bounded
`failure_class`; pending or claimed targets use
`oci_registry_target_collection_pending`; targets that scanned but still lack a
canonical identity use `container_image_identity_scanned_missing`. SBOM gaps
remain separate attachment reasons such as `sbom_attachment_missing`.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'TestServiceStorySupplyChainEvidence(AttachesExactImageAndSBOM|ReportsRepoOnlyHelmValuesImageRef|ExplainsRepoOnlyImageCandidate|ExplainsOCIRegistryTargetOutsideScope|BoundsImageRefLookups)|TestExplainContainerImageCandidateQueryUsesBoundedOCIScopeReadModel|TestContainerImageIdentityQueryUsesActiveFactReadModel' -count=1
cd go && go test ./internal/mcp -run TestDispatchToolServiceStoryPreservesSupplyChainTrace -count=1
cd .. && scripts/test-verify-remote-e2e-target-story.sh
```

Observability Evidence: service-story supply-chain enrichment records a
`supply_chain_evidence` stage through the existing
`service_query.stage_started` and `service_query.stage_completed` log events
with image-ref, evidence, and missing-reason counts. It uses bounded Postgres
read-model list calls plus one repository-id-scoped OCI scope/work-item/warning
explanation query for each missing tagged or digested candidate. It adds no
worker, queue, graph write, metric instrument, metric label, or deployment
knob.

Service story derives `support_overview.spec_count` from the same bounded
`api_surface` aggregate and `spec_paths` evidence used by
`api_surface.spec_count`, so API and MCP readbacks do not report different
OpenAPI spec counts in the same service dossier.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'TestServiceStoryDossierUsesAggregateAPICountsAndSpecPaths|TestGetServiceStorySpecCountsAgreeAcrossAPISurfaceAndSupportOverview' -count=1
cd go && go test ./internal/mcp -run TestDispatchToolServiceStorySpecCountsMatchQueryReadback -count=1
```

No-Observability-Change: the route keeps the existing `service_query.stage_*`
structured stage logs under `operation=service_story`, including
`graph_api_surface`, `service_evidence_content`, `documentation_overview`,
`deployment_evidence`, and `overview_assembly`, plus existing HTTP envelope
truth/error reporting. The change only aligns response synthesis from already
bounded API-surface evidence and adds no graph query, collector call, queue
worker, metric instrument, span name, or deployment knob.

`POST /api/v0/impact/deployment-config-influence` accepts `service_name` or
`workload_id`, optional `environment`, and optional `limit`. Use it when the
caller asks which repositories and files influence image tags, runtime
settings, resource limits, values layers, or rendered Kubernetes resources.
The response preserves `deployment_source_limits` and `k8s_resource_limits` and
folds upstream truncation or lower-bound state into `coverage`. Missing or
inconsistent bound metadata appears in `limitations` and fails coverage closed.
Ambiguous service or workload selectors return HTTP 409. Rendered targets and
image sources are derived only from rows that survived the published bounds.

## Service Investigation

`GET /api/v0/investigations/services/{service_name}` accepts optional
`environment`, `intent`, and `question`.

It returns an investigation packet rather than a polished story: repositories
considered, repositories with evidence, evidence families found, coverage
summary, findings, and recommended next calls. Use it when the caller should
not need to know which deployment, GitOps, Terraform, workflow, support, or
documentation repositories to inspect first.

## Documentation Generation Flow

For generated docs or support prose:

1. Call a story or service investigation route first.
2. Use deployment trace or deployment config influence when deployment details
   matter.
3. Use content routes only after the story identifies exact files, snippets, or
   entity handles worth citing.
