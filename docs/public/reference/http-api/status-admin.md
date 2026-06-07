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

Use `/admin/status` for runtime-local probes. Use `/api/v0/status/*` routes for
public query API status.

## Index Status

- `GET /api/v0/status/index` returns the current checkpoint summary.
- `GET /api/v0/index-status` returns the same checkpoint summary.
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
`reason`, `scope_class`, and `count`. `scope_class` is the public backend class
such as `s3`, `local`, or `unknown`. The payload also includes bounded
`terraform_state.recent_warnings[]` rows with `source_handle`,
`safe_locator_hash`, source class, and reason for source-level triage. Raw
state locators, bucket names, object keys, and local paths are not included in
the public status payload.

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
`reducer_facts`, aggregate counts, and timestamps:

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

No-Regression Evidence: `cd go && go test ./internal/status ./internal/query ./internal/storage/postgres ./internal/mcp -run 'Test(RenderStatusIncludesCollectorRuntimeCategories|CollectorRuntimeStatuses(MergesPersistedFactEvidence|MapsUnattributedFactsToSingleCoordinatorInstance)|StatusHandlerCollectorsRouteExposes(DirectRuntimeEvidence|PersistedFactEvidence)|ReadCollectorFactEvidenceUsesBoundedActiveFactMetadata|ListCollectorsRuntimeToolRoutesToStatusCollectors)' -count=1`.
No-Observability-Change: collector status classification reuses existing
`/admin/status`, `/api/v0/status/collectors`, `aws_cloud_scans`,
`vulnerability_sources`, workflow coordinator rows, active fact metadata, and
MCP HTTP dispatch; it adds one bounded Postgres aggregate status read and does
not add a worker, queue, graph query, or new metric label.

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

Series are sourced from the Prometheus/Mimir collector. When no metrics source
is configured the response is **empty `points` with `truth.freshness.state` of
`unavailable`** rather than an error, and a metric with no history yet returns
empty `points` with `building` freshness. This lets the console show real
point-in-time numbers and enable trend lines only once history exists.

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
