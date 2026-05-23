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

Run-scoped completeness routes such as `/api/v0/index-runs/{run_id}` are not
part of the shipped public contract.

## Pipeline, Collector, And Ingester Status

- `GET /api/v0/status/pipeline`
- `GET /api/v0/status/ingesters`
- `GET /api/v0/status/ingesters/{ingester}`
- `GET /api/v0/ingesters`
- `GET /api/v0/ingesters/{ingester}`

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
