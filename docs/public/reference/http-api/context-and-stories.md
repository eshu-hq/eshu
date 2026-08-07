# HTTP Context And Story Routes

Use this page for entity resolution, context reads, incident/work-item evidence,
catalog navigation, and response rules shared by story and deployment routes.

## Route Map

| Area | Routes or reference |
| --- | --- |
| Entity resolution | `POST /api/v0/entities/resolve` |
| Context | `GET /api/v0/entities/{entity_id}/context`, `GET /api/v0/workloads/{workload_id}/context`, `GET /api/v0/services/{service_name}/context`, `GET /api/v0/repositories/{repo_id}/context` |
| Incident context | `GET /api/v0/incidents/{incident_id}/context` |
| Work-item evidence | `GET /api/v0/work-items/evidence` |
| Catalog | `GET /api/v0/catalog` |
| Stories, intelligence report, and investigation | [Story routes](story-routes.md) |
| Deployment trace and configuration influence | [Deployment trace and influence](deployment-trace-and-influence.md) |

OpenAPI remains canonical for full request and response schemas.

## Entity Resolution

The entity resolution route accepts `name`, optional `type`, optional
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

Workload and service context also carry `limitations` on the primary
graph-materialized path, not only the read-model fallback above: once a
repository is resolved, an auxiliary infrastructure read that fails degrades
the response to a 200 with an empty `infrastructure` list and appends
`infrastructure_read_degraded`, and a healthy infrastructure read that lands
past its bound appends `infrastructure_truncated`. `partial_reasons` promotes
these same reasons into its sorted, de-duplicated array.

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

See [deployment trace relationships](deployment-trace-and-influence.md#deployment-trace-relationships)
for the topology, cloud-resource, and bound contracts.

### Tech Fingerprint Rollup

Repository and service context responses include two additive
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

The incident context route returns a bounded incident
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

The work-item evidence route lists active Jira/work-item source facts.
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

The catalog route is the bounded navigation surface for Console and MCP
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

See the [intelligence report contract](story-routes.md#intelligence-report).

## Stories

See [story response details](story-routes.md#story-response-details).

## Shared Response Contract

Programmatic clients that need route-to-route comparison should request the
canonical envelope with `Accept: application/eshu.envelope+json`. Repository,
entity, workload, and service context/story routes then return `data`, `truth`,
and `error` at the top level. Plain HTTP clients that do not request the
envelope keep the legacy route payload shape.

### Evidence boundaries

Repository, workload, and service story responses and deployment trace may
each include `evidence_boundaries`: a static, closed-vocabulary array of
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
every service story response. `evidence_graph` alone still omits ci_cd/supply-chain
graph edges — it is built from the workload context rather than a BUILT_FROM
graph read, so container_image_identity's BUILT_FROM edges (projected since
issue #5457) are not wired into it — but that narrower sub-surface gap is not
disclosed as a whole-route boundary.

### Hostname classification

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

### Cross-repository truncation

`dependents_truncated`, `consumer_repositories_truncated`, and
`provisioning_source_chains_truncated` report the same class of incomplete
discovery for the cross-repository fan-out. Each is present and true when the
read underneath the matching list hit a bound, and absent otherwise. They are
required because the reads that feed those lists are bounded at 25 rows by
default while the rendered lists are capped at 50, so a genuinely truncated
read can never surface through a count-versus-limit comparison alone.

Six cardinality bounds feed these flags — numeric caps on how many rows or
items survive a step. They are the provisioning-candidate graph read, the
service evidence file read that every observed hostname is extracted from, the
four-hostname cap on the surviving hostname set, the cut that applies when that
set still exceeds the caller's requested limit, each per-search content row cap,
and the final merge cap over the combined set. One filter that is not a
cardinality bound is disclosed alongside them: the hostname affinity filter,
which keeps only hostnames carrying a distinctive token from the service's own
name. It decides which hostnames are searched rather than how many survive, but
it drops reachable consumer repositories exactly the way the four-hostname cap
does, so it sets the flag too. `consumer_repositories_truncated` covers all
seven; the other two flags carry the provisioning-candidate graph read only,
which is the only one of the seven that bounds the lists they describe.

Other filters narrow this evidence further upstream and are deliberately not
disclosed, because they decide which content is considered rather than capping
how much of it survives: the file-extension and path-keyword filter that decides
which repository files are read for evidence at all, the classification filter
that searches only unambiguous hostnames, and the hostname extractor's own
false-positive tables. A hostname that appears only in `terraform/main.tf`,
`Dockerfile`, `nginx/nginx.conf`, or `.env.production` is therefore never
searched for, and these flags stay false.

Treat these flags as "the read underneath was bounded", not as "rows were
definitely dropped". Five conditions make them true when nothing was lost, and
all five err toward disclosure: a false claim of completeness is the worse
failure for an evidence-backed answer.

- The provisioning-candidate graph read bounds **rows**, and one repository can
  supply several rows, one per relationship type and reason. Those rows are
  grouped by repository after the bound is applied, so a graph holding 26 rows
  across 3 repositories at the default 25-row bound sets all three flags true
  and still returns all 3 repositories. What was clipped there is the
  `relationship_types` and `relationship_reasons` metadata inside an entry, not
  the entry itself.
- A per-search content read that returns exactly its row cap is reported as
  truncated, because that read carries no over-fetch probe and cannot
  distinguish "exactly the cap" from "the cap plus more".
- A repository with more than 5,000 indexed files reports
  `consumer_repositories_truncated` whether or not the files past that bound
  held any hostname at all, because the bound is on files and the hostnames are
  derived from them.
- Every narrowing on the hostname set (that 5,000-file read, the four-hostname
  cap, the surviving-set cut, and the affinity filter) sets
  `consumer_repositories_truncated` when a **hostname** was dropped, whether or
  not any consumer repository referenced that hostname.
- A scoped token receives flags computed from the pre-authorization read (see
  below), which can fire on rows the caller was never entitled to.

The same three signals drive `result_limits.truncated` on service story,
service context, and workload context/story, plus service investigation
coverage. Both blocks also
report `downstream_read_limit`, the bound that actually fires on the
downstream lists (25 by default). Read it rather than `result_limits.limit` or
`coverage_summary.result_limit`, which report the 50-row rendering cap.

`downstream_read_limit` is not the only bound behind `truncated`, though.
`dependents_truncated` and `provisioning_source_chains_truncated` reflect the
provisioning-candidate graph read alone, so when either one drives
`truncated` to true the honest statement is "more than `downstream_read_limit`
existed". `consumer_repositories_truncated` can also fire from the other six
sources in the enumeration above -- most notably the service repository's own
5,000-file read, reported alongside as `evidence_file_read_limit`. When
`truncated` is true only because of `consumer_repositories_truncated`, and the
returned `dependents`/`consumer_repositories` counts sit under
`downstream_read_limit`, the bound that fired is `evidence_file_read_limit` or
one of its siblings, not `downstream_read_limit` -- "more than the rendering
cap existed" is still wrong, but so is assuming `downstream_read_limit` itself
was exceeded.

A scoped token receives these flags computed from the pre-authorization read.
The backend applies its row bound before the caller's repository grant is
applied, so the flag reflects global row cardinality and is not grant-relative:
it can be true when every row the bound dropped lay outside the caller's grant.
A scoped caller whose entire result is removed by the grant filter still
receives the truncation flags rather than an empty result that reads as
complete.

That makes the flag a coarse cardinality signal over data the caller cannot
read. Because `max_depth` scales the underlying bound (`max_depth` x 10, capped
at 100), a scoped caller who sweeps `max_depth` over 1 through 10 and reads the
flag at each step can place the global provisioning-candidate row count within
about 10 across the range 10 to 100. No repository name or identifier is
exposed, and the caller is already authorized for the service the count hangs
off. The behavior is deliberate: suppressing the flag for scoped callers would
return an empty or clipped answer that reads as complete, which is the worse
failure for a product whose answers are meant to be evidence-backed.

## Service Investigation

See [investigation packets](story-routes.md#investigation-packets).

## Documentation Generation Flow

For generated docs or support prose:

1. Call a story or service investigation route first.
2. Use deployment trace or deployment config influence when deployment details
   matter.
3. Use content routes only after the story identifies exact files, snippets, or
   entity handles worth citing.
