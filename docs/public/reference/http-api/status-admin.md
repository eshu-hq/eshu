# HTTP Status And Admin Routes

Use these routes to answer three different questions:

- Is the process healthy enough to serve?
- Is the latest index checkpoint complete?
- Does an operator need to replay, skip, refinalize, or inspect durable work?

## Health And Readiness

- `GET /health` reports API process health after dependency initialization. It
  does not prove the latest index run finished.
- `GET /healthz`, `GET /readyz`, `GET /admin/status`, and `GET /metrics` are
  the mounted service-local admin surface. Their OpenAPI contract is
  `docs/openapi/runtime-admin-v1.yaml`, not the public `/api/v0` schema.

`/admin/status` includes bounded runtime status sections when those runtimes are
mounted:

- `registry_collectors` reports aggregate OCI and package-registry collector
  status without leaking registry hosts, repository paths, package names, tags,
  digests, account IDs, metadata URLs, or credentials.
  Package-registry rows include `metadata_targets` counts by ecosystem for
  planned, completed, skipped, stale, failed, and rate-limited metadata work.
  Skipped counts use the stable warning reasons
  `unsupported_metadata_source`, `registry_not_found`, `metadata_too_large`,
  `malformed_metadata`, and `credentials_missing`.
- `aws_cloud_scans` reports scanner status per collector instance, account,
  region, and service kind. It includes commit status, API call count, throttle
  count, warning count, budget and credential flags, and bounded failure-class
  counts. When the result reaches the configured row cap, the response sets
  `aws_cloud_scans_truncated` and reports `aws_cloud_scan_limit`. Raw resource
  IDs, ARNs, event IDs, and raw payloads stay out of the admin contract.
- `aws_freshness` reports AWS Config/EventBridge freshness backlog using
  aggregate status counts, `oldest_queued_age`, and
  `oldest_queued_age_seconds`.
- `semantic_extraction` reports optional LLM-assisted semantic extraction as a
  status field. No-provider mode is `state=unavailable` with
  `reason=provider_not_configured`; code hints and documentation observations
  are disabled, and deterministic indexing, reducer, API, MCP, and docs
  verification paths remain unaffected. When hosted provider profiles are
  configured, `provider_profiles[]` reports redacted profile id, provider kind,
  model id, endpoint profile id, credential source kind, source classes,
  credential/source-policy booleans, and profile state. It never includes raw
  keys, credential handles, prompt text, provider responses, or token-bearing
  endpoint URLs. Semantic queue, budget, and audit readbacks are aggregate-only:
  queue rows are grouped by status, source class, provider kind/profile class,
  policy/guard decision, and failure class; budget rows expose token/cost totals
  and exhaustion counts; audit rows expose actor and ACL classes. Source IDs,
  chunk IDs, fingerprints, raw failures, prompts, provider responses, principals,
  and credentials stay out of these status payloads.
- `collector_generation_dead_letters` reports collector generation commit
  failures that happened before projector work items existed. It includes only
  aggregate counts, replay-requested count, replay attempts, and oldest
  unresolved dead-letter or replay-request age; fact payloads, repository
  names, local paths, credentials, and provider response bodies stay out of the
  admin contract.

Use `/admin/status` for runtime-local probes. Use `/api/v0/status/*` routes for
public query API status.

## Index Status

- `GET /api/v0/status/index` returns the current checkpoint summary.
- `GET /api/v0/index-status` returns the same checkpoint summary.
- `GET /api/v0/status/hosted-readiness` returns a fail-closed hosted operator
  report with JSON by default and a human summary when `?format=text`.
- `GET /api/v0/status/collector-readiness` (alias `/api/v0/collector-readiness`)
  returns the per-collector-family promotion readiness read model: promotion
  state, reducer readback status, evidence counts, last proof time, blockers, and
  a recommended next action. Credentials and raw provider payloads are redacted;
  scoped tokens see family-level readiness only. The MCP tool
  `get_collector_readiness` returns the same shape.
- `GET /api/v0/status/governance` returns redacted hosted governance mode,
  policy state, readiness, aggregate counts, and reason-code readbacks.
- `GET /api/v0/status/semantic-extraction` returns the semantic extraction
  capability status with an Eshu truth envelope when requested by MCP or API
  clients.
- `GET /api/v0/status/answer-narration` returns optional governed answer
  narration status with deterministic answer packets still reported as the
  canonical fallback.
- `GET /api/v0/repositories/{repo_id}/coverage` returns durable repository
  coverage rows for one repository.

The hosted readiness report composes existing status and query-readback signals
without changing Kubernetes probes. It separates process health, dependency
readiness, queue drain, collector completion, shared projection backlog, and
first-query truth. The report returns `state=not_ready` with stable
`failure_classes[]` when the status snapshot is empty, hosted collector
instances are missing, queue work is stalled or not drained, work is
dead-lettered, shared projection backlog remains, graph readback fails, or the
repository readback returns zero rows. It returns `state=ready` only when those
checks pass. Diagnostic pointers reference `/admin/status`,
`/api/v0/status/index`, `/api/v0/status/collectors`, and repository coverage;
raw graph backend errors, resource IDs, account-local names, paths, and
credentials are not included. The MCP equivalent is `get_hosted_readiness`.

The governance status report composes explicit runtime readback from
`ESHU_GOVERNANCE_*` settings and existing semantic aggregate status. It reports
`mode`, `state`, `source_kind`, optional `policy_revision_hash`, readiness
booleans, `identity`, `tenancy`, `egress`, `semantic`, `extensions`,
`redaction`, `retention`, `audit`, aggregate counts, and bounded reason codes.
The `audit` section reports only event, denied, unavailable, event-type,
actor-class, scope-class, reason, and ACL-state counts.
Detailed audit event search is intentionally private until a dedicated query
surface lands. Operators must bound private audit sink searches by event type,
actor class, scope class, decision, reason code, correlation id, and a narrow
time window; raw actor names, tenant names, workspace names, repository paths,
document titles, provider endpoints, prompts, provider responses, credential
handles, and tokens are not valid query or ticket fields. Detailed event bodies
follow the hosted policy retention window in the private audit sink; status and
MCP readbacks retain aggregate counts only.
Local development without governance config reports `local_no_policy` and
`policy_not_configured`; hosted deployments can report `disabled`, `partial`,
`enforcing`, `stale`, or `invalid` without exposing policy bodies. The route
must not expose raw policy JSON, tenant or workspace identifiers, repository or
source identifiers, credential handles, provider endpoints, prompt text,
provider responses, private paths, or token values. The MCP equivalent is
`get_hosted_governance_status`.

No-Regression Evidence: `go test ./internal/governanceaudit ./internal/query -run 'Test(NormalizeEvent|Aggregate|GovernanceStatus)' -count=1` proves hosted governance audit events reject unsafe values without echoing them, aggregate only bounded classes, and expose audit counts through the governance status route.

Observability Evidence: hosted governance audit readbacks reuse
`/api/v0/status/governance`, MCP `get_hosted_governance_status`, and the
existing governance `audit` section; no new metric, span, log, queue, graph, or
storage signal is introduced by this contract slice.

The index status payload includes `aws_materialization`, an aggregate reducer
queue summary for AWS graph/read-model materialization domains. It separates
`pending`, `blocked`, `retrying`, `dead_letter`, `failed`, `in_flight`, and
`outstanding` counts and includes per-domain rows using domain names only. The
`blocked` count is the distinct blocked work-item count for the domain; the
separate `queue_blockages[]` diagnostics keep per-prerequisite rows such as
missing `cloud_resource_uid:canonical_nodes_committed` readiness. Operators can
see named reducer prerequisites alongside aggregate queued, retrying, and
terminal counts without printing ARNs, account-local resource IDs, bucket names,
policy bodies, or other raw AWS payload details.

The payload also includes `terraform_state.warning_summary[]`, empty when no
recent Terraform-state warnings exist. Each row carries `warning_kind`,
`reason`, `scope_class`, `severity`, `actionability`, and `count`.
`scope_class` is the public backend class such as `s3`, `local`, or `unknown`.
`state_missing` and `state_too_large` rows are blocking evidence,
unsupported composites require provider-schema support, and sensitive skips or
accepted normalization rows do not imply collector failure. The payload also
includes bounded `terraform_state.recent_warnings[]` rows with `source_handle`,
`safe_locator_hash`, source class, reason, severity, and actionability for
source-level triage. Raw state locators, bucket names, object keys, and local
paths are not included in the public status payload.

The payload also includes `semantic_extraction`. This mirrors
`/api/v0/status/semantic-extraction` so index-status consumers can tell that
optional semantic extraction is unavailable or disabled without treating it as a
failed index, reducer, API, MCP, or documentation fact path. Configured provider
profiles remain source-policy gated: a profile may report
`credential_configured=true`, but documentation observations and code hints stay
disabled until the matching source class is policy-enabled. When semantic queue
work exists, the same payload includes `queue`, `budget`, and `audit` aggregate
objects so operators can distinguish pending, retrying, dead-lettered,
policy-denied, unsafe, provider-unavailable, and budget-exhausted work without
changing deterministic health.

`/api/v0/status/answer-narration` is a separate optional presentation-status
surface. The default state is unavailable or disabled, with
`deterministic_fallback_available=true` and `canonical_truth_affected=false`.
It reports only low-cardinality state, reason, policy hash, retention posture,
and validator reason-code metadata. It does not generate narration, call
providers, retain prompt or response bodies, mutate answer packets, or expose
source IDs, credentials, private paths, private hostnames, prompts, or provider
responses.

No-Observability-Change: answer narration status is a read-only runtime posture
projection over `internal/status`. It adds no provider call, graph read, content
read, queue, worker, metric, span, log field, prompt construction, response
retention, or answer-packet mutation.

Run-scoped completeness routes such as `/api/v0/index-runs/{run_id}` are not
part of the shipped public contract.

## Pipeline, Collector, And Ingester Status

- `GET /api/v0/status/collectors`
- `GET /api/v0/collectors`
- `GET /api/v0/status/pipeline`
- `GET /api/v0/status/ingesters`
- `GET /api/v0/status/ingesters/{ingester}`
- `GET /api/v0/ingesters`
- `GET /api/v0/ingesters/{ingester}`

`/api/v0/status/collectors` is the canonical collector-status list route.
`/api/v0/collectors` is the legacy GET alias. The response classifies each
collector runtime identity using workflow-coordinator registration, durable
direct status evidence, and active persisted source or reducer fact evidence.
Persisted evidence is returned only as source names such as `source_facts` or
`reducer_facts`, bounded `source_systems`, aggregate counts, and timestamps.
Direct-source collectors can keep a source-neutral collector kind while still
surfacing the real source identity; for example, Confluence documentation facts
appear as `collector_kind=documentation` with `source_systems=["confluence"]`.
Git repository-ingestion facts are included as `collector_kind=git` source-fact
evidence, so a populated repository readback does not leave Git observations at
zero in this operator view:

- `coordinator_managed`: enabled and claim-driven in the workflow coordinator.
- `direct_mode`: registered but claims are disabled.
- `disabled`: registered but disabled or deactivated.
- `unregistered`: direct status or persisted fact evidence exists without a
  matching coordinator registration.
- `profile_gated`: reserved for profile gates that explicitly surface a status
  row.

Central API status cannot discover arbitrary Kubernetes pods by itself. A
deployed collector pod with no coordinator row and no durable direct status row
is an unsupported inventory mode; query that pod's `/admin/status`, metrics, or
the deployment platform inventory to prove process liveness.

`/api/v0/status/ingesters` is the canonical ingester-status list route.
`/api/v0/status/ingesters/{ingester}` is the canonical detail route. The
`/api/v0/ingesters` routes are legacy GET aliases that return the same payload.

## Live Operations Board

- `GET /api/v0/status/operations`

All-scopes operators receive the complete bounded board: process-wide health,
collector runtime, stage, domain-backlog, and queue aggregates plus bounded
live activity. Scoped callers receive only live-activity rows restricted to
their granted repositories or ingestion scopes, with repository and worker
identities redacted. Process-wide aggregates are not tenant- or grant-scoped,
so scoped responses omit `health`, `collectors`, `stage_summaries`,
`domain_backlogs`, and `queue`; they report
`completeness_state=scoped_live_activity_only`, list those names in
`withheld_sections`, and use derived truth. All-scopes responses retain exact
truth. Both legacy JSON and negotiated Eshu envelopes carry the same data
object.

## Operator Control-Plane Read Model

- `GET /api/v0/status/operator-control-plane`

This route returns one operator read model so a responder does not have to
stitch together the pipeline, collector, and dead-letter routes during an
incident. It loads the same status snapshot as `/api/v0/status/pipeline` and
projects it in memory, so it adds no database or graph cost. The MCP tool
`get_operator_control_plane` mirrors the same route and payload.

The response combines:

- `queue`: depth (`total`, `outstanding`, `pending`, `in_flight`, `retrying`,
  `dead_letter`), a `claim_latency` object (`overdue_claims`,
  `oldest_outstanding_age`, `coordinator_oldest_pending`), and a `stuck` object
  (`oldest_outstanding_age`, `blocked_conflicts`).
- `reducer_domains`: per-domain backlog rows, highest pressure first, each
  retaining `retrying`, `dead_letter`, and `oldest_age` for drilldown.
- `collector_families`: one promotion verdict per collector family with
  `promotion_state`, `health`, `claim_state`, `reducer_readback`, the newest
  proof-artifact `last_observed_at`, and stable `telemetry` handles.
- `dead_letters`: `queue_dead_letter` total, a `by_domain` class breakdown,
  the `collector_generation` commit-failure summary, and a `latest_failure`
  object carrying the newest `failure_class` and `domain`.
- `retry_policies`: the active per-stage retry policy summary.

Correlation identifiers (`scope_id`, `generation_id`, `domain`,
`collector_kind`, `failure_class`) match the runtime metric and span labels in
[Telemetry](../telemetry/index.md), so an operator can pivot from a read-model
row to the matching metric series or trace.

Scoped tokens receive the same aggregate counts and ages with a `scoped: true`
flag. Raw `work_item_id`, `scope_id`, and `generation_id` values on
`latest_failure` and instance-level collector labels (`display_name`,
`blockers`) are withheld; the `failure_class`, domain, and every count stay
visible.

The default ingester is `repository`. Status responses include:

- ingester identity
- current status
- active run ID
- last attempt and last success
- next retry timing
- repository progress counts
- failure counts and last error details

The public API does not include a per-ingester scan POST route. Use
`POST /api/v0/admin/reindex` or deployment-managed ingestion instead.

## Component Extension Inventory

- `GET /api/v0/component-extensions?limit=100`
- `GET /api/v0/component-extensions/{component_id}/diagnostics`

These routes expose optional component package inventory and policy diagnostics
from the API or MCP runtime's configured component registry. They read
`ESHU_COMPONENT_HOME` and the component trust environment on that runtime, not
the caller's local CLI state. When `ESHU_COMPONENT_HOME` is unset or the
registry cannot be read safely, the route returns HTTP 503 with the canonical
`component_registry_unavailable` error.

Successful inventory responses are bounded by `limit` (default 100, max 500)
and return `count`, `total_count`, and `truncated` so callers know whether the
page is complete. Rows include component ID, publisher, version, manifest
digest, installed time, installed, enabled, claim-capable, revoked,
incompatible, and failed states, activation instance IDs, activation modes,
claim enablement,
stable `config_handle` values, and policy diagnostics. They do not expose
manifest paths, activation config paths, provider credentials, raw config
payloads, or other server-local host paths. Community extension index
membership is discovery metadata only and is not reported as trust; the trust
verdict comes from local registry readback and the configured policy.

The MCP equivalents are `list_component_extensions` with optional `limit` and
`get_component_extension_diagnostics`. The CLI equivalents are
`eshu component inventory --limit <n> --json` and
`eshu component diagnostics <component-id> --json`.

## Collector Extraction Readiness

- `GET /api/v0/collector-extraction-readiness?limit=100`
- `GET /api/v0/collector-extraction-readiness/{family}`

These routes expose the advisory collector extraction readiness checklist. For
each collector family the extraction policy tracks, they report a classification
(`keep_in_tree`, `extraction_candidate`, `blocked`, or `external_ready`), the
per-criterion checklist, and any blockers. The data is static policy
classification computed from documented repository evidence; the routes read no
runtime, graph, or registry state, and the result is advisory only and never
moves code. The drilldown returns HTTP 404 with the canonical `not_found` error
when the family is not tracked by the policy.

List responses are bounded by `limit` (default 100, max 500) and return `count`,
`total_count`, and `truncated`. See
[Collector Extraction Policy](../collector-extraction-policy.md) for the
classification vocabulary and the seven criteria.

The MCP equivalents are `list_collector_extraction_readiness` with optional
`limit` and `get_collector_extraction_readiness`. The CLI equivalent is
`eshu component extraction-readiness [family] --json --verbose`.

## Fact Schema Versions

- `GET /api/v0/fact-schema-versions?limit=200`
- `GET /api/v0/fact-schema-versions/{fact_kind}?candidate=2.0.0`

These routes expose the core fact-schema-version compatibility registry. The list
returns the schema version a core reducer or query consumer currently supports
for each core fact kind. The drilldown returns the supported version for one
fact kind, and when the `candidate` query parameter is supplied it classifies
that collector version as `supported`, `unsupported_major`, `unsupported_minor`,
or `unknown_kind` — so a client can detect an incompatible collector fact version
safely before its facts are admitted. The drilldown returns HTTP 404 with the
canonical `not_found` error when the fact kind is not a core-owned fact kind with
a registered schema version.

The data is the static in-binary registry from `go/internal/facts`; the routes
read no runtime, graph, or registry state, and the result is advisory only. List
responses are bounded by `limit` (default 200, max 500) and return `count`,
`total_count`, and `truncated`. See
[Fact Schema Versioning](../fact-schema-versioning.md) for the compatibility
contract.

The MCP equivalents are `list_fact_schema_versions` with optional `limit` and
`get_fact_schema_version` with `fact_kind` and optional `candidate`. The CLI
equivalent is `eshu component schema-versions [--check fact_kind=version] --json`.

No-Regression Evidence: `cd go && go test ./internal/query ./internal/mcp -run 'Test(FactSchemaVersion|ReadOnlyToolsIncludesFactSchemaVersion)' -count=1` proves the registry list, drilldown, candidate classification, 404 behavior, and MCP route resolution. The routes read the static `facts.SupportedSchemaVersions()` registry in process and issue no graph, queue, or content reads.
No-Observability-Change: the fact-schema-version routes reuse the existing query
API envelope, truth metadata, and error contract; they add no metrics, spans,
logs, status fields, queue domains, or graph writes.

No-Regression Evidence: `cd go && go test ./internal/status ./internal/query ./internal/storage/postgres ./internal/mcp -run 'Test(RenderStatusIncludesCollectorRuntimeCategories|CollectorRuntimeStatuses(MergesPersistedFactEvidence|MapsUnattributedFactsToSingleCoordinatorInstance)|StatusHandlerCollectorsRouteExposes(DirectRuntimeEvidence|PersistedFactEvidence)|ReadCollectorFactEvidenceUsesBoundedActiveFactMetadata|ListCollectorsRuntimeToolRoutesToStatusCollectors)' -count=1`; `cd go && go test ./internal/status ./internal/query ./internal/storage/postgres -run 'Test(CollectorRuntimeStatusesMergesPersistedFactEvidence|StatusHandlerCollectorsRouteExposesPersistedFactEvidence|ReadCollectorFactEvidenceUsesBoundedActiveFactMetadata)' -count=1` proves source systems survive persisted fact evidence, status projection, and public collector status rendering.
No-Observability-Change: collector status classification reuses existing
`/admin/status`, `/api/v0/status/collectors`, `aws_cloud_scans`,
`vulnerability_sources`, workflow coordinator rows, active fact metadata, and
MCP HTTP dispatch; it adds one bounded Postgres aggregate status read and does
not add a worker, queue, graph query, or new metric label.

No-Regression Evidence: `cd go && go test ./internal/query ./internal/mcp -run 'Test(StatusHandlerHostedReadiness|OpenAPISpecStatusPathsMatchCurrentContract|HostedReadinessRuntimeToolRoutesToStatus|SummarizePlainToolTextHostedReadiness|ToolDefinitionsIncludeExpectedTools)' -count=1` proves hosted readiness fail-closed states, text summary, OpenAPI coverage, and MCP routing stay aligned.
No-Observability-Change: hosted readiness reuses existing `/admin/status`,
`/api/v0/status/index`, `/api/v0/status/collectors`, repository coverage, and
the bounded graph repository count read used by index status. It adds no
runtime, worker, queue, metric label, or new provider/backend read beyond that
operator-facing report composition.

No-Regression Evidence: `cd go && go test ./internal/semanticqueue ./internal/storage/postgres ./internal/status ./internal/query ./internal/telemetry -count=1` proves semantic queue lifecycle fencing, redacted aggregate status, OpenAPI, API envelopes, and telemetry contracts stay in sync.
Observability Evidence: semantic extraction status now exposes aggregate
`queue`, `budget`, and `audit` readbacks through `/admin/status`,
`/api/v0/status/index`, `/api/v0/status/semantic-extraction`, and MCP dispatch.
The data-plane telemetry contract includes
`eshu_dp_semantic_extraction_queue_events_total`,
`eshu_dp_semantic_extraction_budget_tokens_total`, and
`eshu_dp_semantic_extraction_budget_cost_micros_total` with bounded labels only:
`source_class`, `provider_kind`, `provider_profile_class`, `status`,
`failure_class`, `budget_state`, and `budget_reason`.

## Generation Lifecycle Drilldown

`GET /api/v0/freshness/generations` returns a bounded, ordered page of scope
generation lifecycle rows so operators and agents can inspect active, pending,
superseded, completed, and failed generation history without scraping the broad
pipeline status payload.

Filters (all optional; supply a scope selector for a scoped answer):

- `scope_id` exact ingestion scope id
- `repository` canonical repository id (matches repository-kind scopes by
  `source_key`)
- `collector_kind`, `source_system`
- `generation_id` exact generation
- `status` one of `pending`, `active`, `superseded`, `completed`, `failed`

The read is bounded by `limit` (default 50, max 500) and ordered by
`observed_at DESC, generation_id ASC`. The handler fetches `limit+1` rows to set
`truncated`. Each record carries the scope identity (`scope_kind`,
`source_system`, `collector_kind`), the scope's `current_active_generation_id`,
`is_active`, `trigger_kind`, `freshness_hint`, the observed/ingested/activated/
superseded timestamps, the per-generation `queue_status` rollup
(`total`, `outstanding`, `in_flight`, `retrying`, `succeeded`, `failed`,
`dead_letter`), and `latest_failure` (`failure_class`, `failure_message`) when a
work item for that generation recorded a failure.

A named `scope_id`, `repository`, or `generation_id` selector that matches
nothing returns an explicit `scope_not_found` or `not_found` error instead of an
empty list. The truth envelope marks `freshness.state=building` when a returned
scope has a pending or in-flight generation. The capability key is
`freshness.generation_lifecycle`. The MCP equivalent is `get_generation_lifecycle`
and the CLI helper is `eshu freshness generations`.

No-Regression Evidence: `cd go && go test ./internal/status ./internal/storage/postgres ./internal/query ./internal/mcp ./cmd/eshu ./cmd/api -count=1` proves the generation lifecycle types, bounded Postgres read, query handler envelope/not-found behavior, MCP route, CLI envelope, and API wiring stay in sync.
No-Observability-Change: the drilldown adds one bounded Postgres read joining `scope_generations`, `ingestion_scopes`, and `fact_work_items`, plus the existing `query.freshness_generation_lifecycle` span with low-cardinality result-count, truncated, active-count, and failure-count attributes; it adds no worker, queue, graph query, or new metric label.

## Changed-Since Delta

`GET /api/v0/freshness/changed-since` answers "what changed in this repository
scope since a prior generation or instant?" without re-indexing. It diffs the
prior generation's fact set against the scope's current active generation's fact
set, keyed by `stable_fact_key`.

Required parameters:

- exactly one mutually exclusive scope selector: `scope_id` (exact) **or**
  `repository` (canonical repository id, matches repository-kind scopes by
  `source_key`). Supplying both is a bad request; the server never intersects
  an old scope id with a newly selected repository.
- a since reference: `since_generation_id` (exact prior generation) **or**
  `since_observed_at` (RFC3339; the diff baseline is the generation observed at or
  before that instant)

Optional `sample_limit` (default 25, max 200) caps the per-classification sample
handles. The response carries the resolved `scope_id`, `scope_kind`,
`since_generation_id`/`since_observed_at`, `current_active_generation_id`,
`current_observed_at`, and a `categories` array. Each category (`files`,
`content_entities`, `facts`) carries exact `counts` for `added`, `updated`,
`unchanged`, `retired`, and `superseded`, plus bounded `samples`
(`stable_fact_key`, `fact_kind`) per classification and a per-classification
`truncated` flag. `added` is a key new in the current generation; `updated` is a
key in both whose `md5(payload)` differs; `unchanged` is a key in both with an
identical payload hash; `retired` is a key tombstoned in the current generation;
`superseded` is a key dropped entirely on generation rollover. Retired and
superseded are never collapsed into `unchanged`.

The response includes the resolved canonical `repository` even when the caller
uses a legacy `scope_id` selector. An unknown `scope_id`/`repository` returns
`scope_not_found`; a since reference
that resolves to no generation returns `not_found`; a scope with no current
active generation returns `unavailable=true` (and a `building`/`unavailable`
freshness state) rather than zero deltas. If generation retention proves the
prior generation was pruned, the response keeps `unavailable=true`, sets
`unavailable_reason=retention_expired`, and the truth freshness `next_check`
points to `get_generation_lifecycle` / `GET /api/v0/freshness/generations`.
Counts are exact; only the samples are bounded. The capability key is
`freshness.changed_since`. The MCP equivalent is `get_changed_since` and the CLI
helper is `eshu freshness changed-since`.

### Service-scope changed-since

`GET /api/v0/freshness/services/changed-since` answers "what changed for this
service since a prior service materialization generation?" A service is not an
ingestion scope, so this surface diffs a per-service generation lineage
(`service_materialization_generations`, one active generation per `service_id`)
over generation-stable evidence snapshots (`service_evidence_snapshots`) keyed by
a generation-independent `service_evidence_key` (for example
`ownership:<service_id>:<owner_ref>`, `deployment:<service_id>:<identity>`
(where the deployment identity is a digest of the resolved deployment
relationship's generation-independent natural key),
`runtime:<service_id>:<platform_kind>:<environment>:<workload_ref>` (where
`workload_ref` is the durable `WorkloadInstance` id, which carries no resolution
or materialization generation id), or `dependencies:<service_id>:<identity>`
(where the dependency identity is a digest of the resolved dependency
relationship's generation-independent natural key — `DEPENDS_ON` / `USES_MODULE`
/ `READS_CONFIG_FROM` — and, like deployment, its `resolved_id` embeds the
resolution generation and is therefore not a stable diff key), or
`incidents:<service_id>:<provider>:<provider_incident_id>:<slot>:<evidence_kind>:<evidence_id>`
(one durable routing identity per PagerDuty incident-routing slot, where
`evidence_id` is the source fact's generation-independent `StableFactKey` or
durable content-entity id, never the generation-bearing envelope `FactID`).

Required parameters: `service_id` (exact) and `since_generation_id` (a prior
service generation id). Optional `sample_limit` (default 25, max 200) caps the
per-classification sample handles. The response carries the resolved
`service_id`, `since_generation_id`, `current_active_generation_id`, and a
`categories` array. The surface reports the `ownership` (#1943), `deployment`
(#1985), `runtime` (#1986), `dependencies` (#1987), `docs` (#1988),
`incidents` (#1989), and `vulnerabilities` (#1990) families. Each category carries
exact `counts` for `added`, `updated`, `unchanged`, `retired`, and `superseded`,
plus bounded `samples` (`stable_fact_key` carrying the `service_evidence_key`,
`fact_kind` carrying the evidence family) per classification and a
per-classification `truncated` flag. The classification, `md5`-based
updated-vs-unchanged detection, and explicit retirement match the repository-scope
surface; retired and superseded are never collapsed into `unchanged`.

An unknown `service_id` returns `service_not_found`; an unresolved
`since_generation_id` returns `not_found`; a service with no current active
generation returns `unavailable=true` (and a `building`/`unavailable` freshness
state) rather than zero deltas. The capability key is
`freshness.service_changed_since`. The MCP equivalent is
`get_service_changed_since` and the CLI helper is `eshu freshness
service-changed-since`.

The incidents family's production loader is held behind a durable
PagerDuty-provider-to-Eshu-catalog service-id join that is a tracked #1989
follow-up, and the vulnerabilities family's loader is held behind a durable
service-to-repository-to-package-to-advisory join that is a tracked #1990
follow-up, so their rows materialize once those joins exist. All six service
evidence families now ship the emitter, category, delta surface, and a
nil-tolerant loader seam.

Performance Evidence: the diff is bounded by the requested `sample_limit` and
keyed by `(scope_id, generation_id, stable_fact_key)`. Counts come from one
aggregate over `fact_records` filtered to the two generations of one scope, using
the `fact_records_scope_generation_idx` index (`scope_id, generation_id`
anchored) for each per-generation scan and a hash join on `stable_fact_key` to
classify keys; sample reads run only for non-empty classification buckets and
each is `ORDER BY stable_fact_key LIMIT sample_limit+1`. (The prior
`fact_records_stable_key_idx` was dropped in #4859 — `EXPLAIN` on the live stack
confirms this query anchors on `fact_records_scope_generation_idx` and hash-joins
by `stable_fact_key` rather than probing a `stable_fact_key`-leading index.) Expected cardinality scales with
the per-generation fact count of a single repository scope (files plus content
entities plus facts), not the whole repository corpus; no whole-graph or
cross-scope scan is performed. Live SQL is exercised by the CI integration gate
against Postgres; the in-process fake `Queryer`/`Rows` harness
(`internal/storage/postgres/changed_since_test.go`) proves the bounding, diff
classification, truncation, and resolution-failure behavior.

No-Observability-Change: the surface adds the bounded
`query.freshness_changed_since` span with low-cardinality scope-id,
since-generation, current-generation, changed-count, and unavailable attributes;
it adds no worker, queue, graph query, or new metric label.

## Historical Metrics

`GET /api/v0/metrics/timeseries?metric={name}&window={24h}&step={30m}` returns an
ordered point series for one metric, for dashboard and operations trend charts.
Supported `metric` values are `ingest_rate`, `queue_depth`, `dead_letters`,
`graph_nodes`, `graph_edges`, `query_p50`, `query_p95`, and `query_p99`; an
unsupported or missing `metric` returns a `400`. `window` defaults to `24h`,
must be at most `30d`, and `step` defaults to `30m`. `step` must be at least
`10s`, and `window / step` must request at most 2,000 samples. The response
carries `metric`, `unit`, `window`, `step`, and an ordered `points: [{ t, v }]`
array.

### Trend Source Requirements

These series are read at request time from a Prometheus-compatible
`query_range` API. The API reuses the enabled `prometheus_mimir` collector
instance from `ESHU_COLLECTOR_INSTANCES_JSON` as that read source (selected by
`ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID` when more than one is enabled).
The PromQL expressions query Eshu's **own** data-plane self-metrics, including
`eshu_dp_facts_committed_total`, `eshu_dp_queue_depth`,
`eshu_dp_canonical_nodes_written_total`, `eshu_dp_canonical_edges_written_total`,
and `eshu_http_request_duration_seconds_bucket`.

For the trend charts to return points, the configured `base_url` must point at a
Prometheus or Mimir that **scrapes Eshu's `/metrics` endpoints** (default port
`9464`, gated by `ESHU_PROMETHEUS_METRICS_ENABLED`). This is a different concern
from what the `prometheus_mimir` *collector* does: the collector reads metrics
*from* an external monitoring system *into* the Eshu graph. Pointing the trend
source at a customer or observability Prometheus that does not scrape Eshu's own
metrics yields a **successful query with zero matching series**, so the charts
stay empty even though a source is wired. Wiring the trend source and scraping
Eshu's self-metrics into it is a deployment-time configuration decision, not an
application default.

### Empty-State Freshness

The endpoint never fabricates points. The response distinguishes three states
through `truth.freshness.state`:

| State | Meaning | `truth.level` |
| --- | --- | --- |
| `unavailable` | No metrics source is configured. | `fallback` |
| `building` | A source is configured and the query succeeded, but no samples matched yet — the metric has no history, or the Prometheus does not scrape Eshu's self-metrics. | `derived` |
| `fresh` | The query returned one or more points. | `derived` |

The console correctly degrades to an empty trend with the "Trend history appears
when a Prometheus/Mimir metrics source has recent samples" message for both
`unavailable` and `building`. An empty chart on a stack whose trend source
points at a Prometheus that does not scrape Eshu is expected behavior, not a
console or API defect.

## Durable Admin Controls

- `POST /api/v0/admin/refinalize` re-enqueues active scope generations for
  projection through the durable Go work queue.
- `POST /api/v0/admin/reindex` persists an asynchronous reindex request. The
  API process does not run the full reindex inline.
- `POST /api/v0/admin/recover-generations` is the operator escape hatch for
  generations that wedge `active` without advancing past
  canonical-nodes-committed. It durably re-enqueues projector work for the named
  scopes (re-driving reduce -> readiness -> projection over existing facts, no
  re-clone) and records the action in the durable `admin_replay_requests` ledger.
- `GET /api/v0/admin/shared-projection/tuning-report` returns the operator
  tuning report for shared-projection backlog behavior.
- `POST /api/v0/admin/replay`
- `POST /api/v0/admin/dead-letter`
- `POST /api/v0/admin/skip`
- `POST /api/v0/admin/backfill`
- `POST /api/v0/admin/work-items/query`
- `POST /api/v0/admin/decisions/query`
- `POST /api/v0/admin/replay-events/query`

The recovery handler owns replay, dead-letter, skip, backfill, and decision
inspection. Mount those controls only on runtimes that are allowed to operate
the durable queue.

### Safe Replay Workflow

`POST /api/v0/admin/replay` is a guarded, auditable recovery action:

- **Explicit reason and idempotency key are required.** The request must carry a
  `reason` (why the replay is safe) and an `idempotency_key`. Missing either is
  a `400`.
- **Authorization gate.** Replay requires an admin (all-scopes) token. A
  scoped/limited token is refused with `403`.
- **Unsafe classes are refused.** Terminal items whose `failure_class` is
  non-retryable (`input_invalid`) or quarantined (`unsafe_payload`) are excluded
  from broad replays, and an explicit unsafe-class request returns `422` with
  actionable guidance unless `force=true` is set after addressing the cause.
- **Concurrent and duplicate delivery are handled.** The `idempotency_key` is
  recorded in the durable `admin_replay_requests` ledger whose primary key
  serializes concurrent requests: exactly one request runs the replay, and a
  duplicate with the same key returns the prior outcome (`duplicate=true`)
  instead of replaying again. A key reused with different selectors, or one whose
  replay is still in progress, returns `409`.
- **Audit without secrets.** Each accepted or refused replay records a governance
  `admin_recovery_action` audit event (actor class, admin scope class, decision,
  reason code) carrying no work-item, scope, generation, or payload identifiers.

The CLI mirrors this: `eshu admin facts replay --reason "<why>" [--idempotency-key
<key>] [--scope-id <id>] [--stage <stage>] [--failure-class <class>] [--force]`.
A random idempotency key is generated when one is not supplied.

### Wedged Generation Recovery

A scope generation that reaches `active` but never advances past
canonical-nodes-committed sits `active` indefinitely if no newer generation
arrives to supersede it. Two mechanisms protect against this:

- **Self-healing (reducer liveness sweep).** The reducer runs a lease-light,
  periodic sweep (`GenerationLivenessRunner`) that flags `active` generations
  whose `activated_at` is older than the activation deadline, that have no newer
  same-scope generation, **and that still have real downstream blockage** — an
  outstanding `shared_projection_intents` row (`completed_at IS NULL`) after
  reducer fact-work for the same generation has drained, with no source-local
  projector row already pending or in progress — then durably
  re-enqueues projector work to re-drive them. Age alone is not enough: a
  healthy quiet scope normally stays `active` and projected (the projected
  baseline is "has been active") with every intent completed, a busy full-corpus
  bootstrap scope can have outstanding shared intents while reducer work is
  still legitimately progressing, and a liveness recovery row that is already
  pending or in progress should be processed before the sweep spends more
  budget. Those cases are excluded and not re-driven. A succeeded source-local
  projector row is the normal activation lifecycle state, so the bounded
  recovery upsert may reopen it when downstream blockage still makes the
  generation wedged. A bounded per-generation re-drive budget
  (`liveness_recovery_attempts`, stored on the work item payload) prevents a
  poison scope from looping forever. The sweep also supersedes orphaned older
  `active` generations once a newer same-scope generation is authoritative.
  Tune it with `ESHU_GENERATION_LIVENESS_*` (enabled, poll interval, activation
  deadline, max recover attempts, batch limit). It is enabled by default.
- **Operator escape hatch.** `POST /api/v0/admin/recover-generations` re-drives a
  named set of wedged scopes on demand. Like replay it requires `scope_ids`, an
  explicit `reason`, and an `idempotency_key`; requires an admin (all-scopes)
  token; records the action in the durable `admin_replay_requests` ledger; and
  returns the prior outcome (`duplicate=true`) for a repeated key. It re-enqueues
  projector work over existing facts — no re-clone — so a wedged scope is driven
  through canonical-nodes-committed -> completed -> projected.

Observability for wedged generations:

- `eshu_dp_active_generations` is a gauge of current active generations by
  closed activation-age bucket (`fresh`, `aging`, `stuck`). The `stuck` bucket
  matches the recovery gate: a generation is only `stuck` when it has aged past
  the deadline AND has outstanding `shared_projection_intents`
  (`completed_at IS NULL`) AND has no unresolved reducer fact-work for the same
  generation AND has no source-local projector row already pending or in
  progress. A healthy quiet scope that merely aged, a scope still moving through
  reducer backlog, or an in-flight liveness recovery row is counted `aging`,
  never `stuck`, so a non-zero `stuck` bucket avoids false alarms on idle
  installations and normal bootstrap backlog while still surfacing blocked
  active generations.
- `eshu_dp_generation_liveness_recovered_total` and
  `eshu_dp_generation_liveness_superseded_total` count what the sweep re-drove
  and retired; `eshu_dp_generation_liveness_failures_total` counts sweep errors
  by bounded reason.

<!-- Evidence for issue #3478 generation-lifecycle recovery. -->

No-Regression Evidence: The liveness sweep adds one new reducer side-runner that
runs two bounded statements per poll (default 5m): a supersede UPDATE bounded by
`LIMIT $2` over `scope_generations` partitioned by `scope_id` (using the existing
`scope_generations_active_scope_idx`), and a wedged-detection re-enqueue bounded by
`LIMIT $3` that re-uses the projector `fact_work_items` upsert (`ON CONFLICT
(work_item_id) DO UPDATE`). No new hot-path Cypher or graph write is introduced;
the re-drive re-uses the existing projector enqueue path. Wedge detection and the
`stuck` age bucket gate on real downstream blockage via an `EXISTS` subquery over
`shared_projection_intents` keyed by `generation_id` and the partial index
`shared_projection_intents_*_pending_idx` (`WHERE completed_at IS NULL`), plus a
same-generation reducer fact-work exclusion so normal reducer backlog is treated
as progress rather than a wedge, and a source-local projector exclusion so
in-flight recovery is not reopened before the projector processes it. Healthy
quiet projected scopes and busy full-corpus bootstrap scopes are excluded. A
succeeded source-local projector row remains eligible for the bounded upsert
because that is the normal activation lifecycle state for a generation that may
still be wedged downstream. The conflict domain is `scope_id`; both statements
are idempotent under concurrent reducer workers (a second sweep finds no
remaining wedged/orphaned row) and bound the per-generation re-drive
budget via `liveness_recovery_attempts` so a poison scope cannot loop. No
worker-count, batch-size, or queue-default change. The active-by-age gauge reads a
single bounded `GROUP BY age_bucket` aggregate over active rows with the same
shared-intent and reducer-backlog gates for the stuck bucket per metrics scrape.
Baseline: no liveness sweep existed (active generations wedged indefinitely:
observed active=982 frozen). After: wedged actives are re-driven within the
activation deadline (default 30m) under unit and race tests. Full before/after
`scope_generations` counts require a live cluster and are recorded as the
operational verification step below.

Observability Evidence: New metrics `eshu_dp_active_generations` (gauge by
fresh/aging/stuck age bucket; `stuck` is the wedged-generation alarm signal),
`eshu_dp_generation_liveness_recovered_total`,
`eshu_dp_generation_liveness_superseded_total`, and
`eshu_dp_generation_liveness_failures_total` (by bounded reason), plus structured
logs `generation liveness recovery cycle completed` / `... failed` with the
`reduction` phase attribute. The `recover-generations` endpoint records a
governance `admin_recovery_action` audit event and writes the
`admin_replay_requests` ledger.

Operational verification (requires a live cluster, not runnable here): capture
`scope_generations` status counts (active/superseded/pending/completed) and
`pending_projection.outstanding` before; enable the reducer (liveness on by
default); confirm `eshu_dp_active_generations{age_bucket="stuck"}` drops and
`eshu_dp_generation_liveness_recovered_total` rises as wedged actives advance to
completed/projected; for a targeted scope, `POST /api/v0/admin/recover-generations`
with `scope_ids`, `reason`, and `idempotency_key`, then confirm
`admin_replay_requests` has the row and the scope drains through
canonical-nodes-committed -> completed -> projected.
