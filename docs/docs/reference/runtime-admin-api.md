# Runtime Admin API

Use this page when you need the operator-facing HTTP contract for the
long-running Go runtimes rather than the public `/api/v0` query API.

## Contract Split

Eshu currently has two HTTP contract families:

- Public query API:
  served by the Go API runtime and published at `/api/v0/openapi.json`
- Public API admin routes:
  served by the same API runtime under `/api/v0/admin/*`
- Runtime admin API:
  a separate mounted runtime surface for probes and operator status

The runtime admin API is intentionally **not** part of the public `/api/v0`
OpenAPI document because it is served by individual long-running runtimes such
as the ingester, projector, and reducer processes.

The checked-in OpenAPI contract for that runtime surface lives at:

`docs/openapi/runtime-admin-v1.yaml`

The runtime admin API is distinct from the public API admin routes. The public
API exposes the `/api/v0/admin/*` control and inspection surface in
`/api/v0/openapi.json`, while this page covers the service-local `/admin/*`
endpoints mounted by a long-running runtime itself.

## Mounted Endpoints

Every runtime that mounts the shared admin surface exposes:

- `GET` and `HEAD` `/healthz`
- `GET` and `HEAD` `/readyz`
- `GET` and `HEAD` `/admin/status`

When the runtime also mounts a metrics handler, it exposes:

- `GET` and `HEAD` `/metrics`

Runtimes that mount the Go recovery handler also expose:

- `POST` `/admin/replay`
- `POST` `/admin/refinalize`

Current runtime shape:

- API: shared probes, status, and metrics
- MCP server: shared probes, status, and metrics
- Ingester: shared probes, status, metrics, plus recovery routes
- Reducer: shared probes, status, and metrics

The shared runtime metrics families are documented in
[Telemetry Metrics](telemetry/metrics.md).

Unsupported verbs return `405 Method Not Allowed` with an `Allow` header
listing the methods supported by that endpoint. For GET/HEAD-only endpoints
(`/healthz`, `/readyz`, `/admin/status`), the header is `Allow: GET, HEAD`.
When recovery routes are mounted, `/admin/replay` and `/admin/refinalize`
return `Allow: POST` for all non-`POST` requests.

If you need the public API admin surface instead of the runtime-local one, use
the `/api/v0/openapi.json` contract for:

- `POST /api/v0/admin/refinalize`
- `POST /api/v0/admin/reindex`
- `GET /api/v0/admin/shared-projection/tuning-report`
- `POST /api/v0/admin/replay`
- `POST /api/v0/admin/dead-letter`
- `POST /api/v0/admin/skip`
- `POST /api/v0/admin/backfill`
- `POST /api/v0/admin/work-items/query`
- `POST /api/v0/admin/decisions/query`
- `POST /api/v0/admin/replay-events/query`

## Response Shape

`/admin/status` supports:

- `format=text`
- `format=json`
- `Accept: application/json` when `format` is omitted

`HEAD /admin/status` follows the same format-selection rules as `GET`, but it
returns headers and status code only.

The JSON response follows the shared status report shape from
`go/internal/status/`:

- `as_of`
- `health`
- `flow`
- `queue`
  The queue payload distinguishes `dead_letter` rows from `failed` rows when
  both appear in the same snapshot.
- `retry_policies`
- `scope_activity`
- `generation_history`
- `generation_transitions`
- `scopes`
- `generations`
- `stages`
- `domains`
- `queue_blockages`

Queue entries include both a duration string and seconds value:

- `oldest_outstanding_age`
- `oldest_outstanding_age_seconds`

Domain entries include both a duration string and seconds value:

- `oldest_age`
- `oldest_age_seconds`

`queue_blockages` entries are present when reducer work is visible and
claimable except for an active coordination gate on the same conflict domain.
They include:

- `stage`
- `domain`
- `conflict_domain`
- `conflict_key`
- `blocked`
- `oldest_age`
- `oldest_age_seconds`

Retry policy entries include:

- `stage`
- `max_attempts`
- `retry_delay`
- `retry_delay_seconds`

Generation transition entries include:

- `scope_id`
- `generation_id`
- `status`
- `trigger_kind`
- `freshness_hint`
- `observed_at`
- `activated_at`
- `superseded_at`
- `current_active_generation_id`

## Runtime Ownership

The shared mounted runtime admin surface is implemented through:

- `go/internal/status/http.go` for report rendering
- `go/internal/runtime/admin.go` for the shared probe/admin mux
- `go/internal/runtime/status_server.go` for mounted status-server wiring
- `go/internal/runtime/http_server.go` for lifecycle-owned HTTP serving
- `go/internal/runtime/recovery_handler.go` for optional recovery mounts

That means new runtimes should not build bespoke probe or status endpoints.
They should reuse the shared mounted contract and only provide the service-
specific backing reader, metrics handler, and optional recovery handler. The Go
runtime is the source of truth for mounted recovery and status behavior today.
