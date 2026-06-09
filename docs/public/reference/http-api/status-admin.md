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

Use `/admin/status` for runtime-local probes. Use `/api/v0/status/*` routes for
public query API status.

## Index Status

- `GET /api/v0/status/index` returns the current checkpoint summary.
- `GET /api/v0/index-status` returns the same checkpoint summary.
- `GET /api/v0/status/semantic-extraction` returns the semantic extraction
  capability status with an Eshu truth envelope when requested by MCP or API
  clients.
- `GET /api/v0/repositories/{repo_id}/coverage` returns durable repository
  coverage rows for one repository.

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

No-Regression Evidence: `cd go && go test ./internal/status ./internal/query ./internal/storage/postgres ./internal/mcp -run 'Test(RenderStatusIncludesCollectorRuntimeCategories|CollectorRuntimeStatuses(MergesPersistedFactEvidence|MapsUnattributedFactsToSingleCoordinatorInstance)|StatusHandlerCollectorsRouteExposes(DirectRuntimeEvidence|PersistedFactEvidence)|ReadCollectorFactEvidenceUsesBoundedActiveFactMetadata|ListCollectorsRuntimeToolRoutesToStatusCollectors)' -count=1`; `cd go && go test ./internal/status ./internal/query ./internal/storage/postgres -run 'Test(CollectorRuntimeStatusesMergesPersistedFactEvidence|StatusHandlerCollectorsRouteExposesPersistedFactEvidence|ReadCollectorFactEvidenceUsesBoundedActiveFactMetadata)' -count=1` proves source systems survive persisted fact evidence, status projection, and public collector status rendering.
No-Observability-Change: collector status classification reuses existing
`/admin/status`, `/api/v0/status/collectors`, `aws_cloud_scans`,
`vulnerability_sources`, workflow coordinator rows, active fact metadata, and
MCP HTTP dispatch; it adds one bounded Postgres aggregate status read and does
not add a worker, queue, graph query, or new metric label.

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
