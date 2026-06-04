# Runtime Admin API

Use this page for the service-local HTTP surface mounted by long-running Go
runtimes. This is separate from the public `/api/v0` query API and separate
from public API admin routes under `/api/v0/admin/*`.

The checked-in OpenAPI contract for this surface is
`docs/openapi/runtime-admin-v1.yaml`. The Go implementation lives in
`go/internal/runtime` and `go/internal/status`.

## Mounted Endpoints

Every runtime that uses the shared admin mux exposes:

- `GET` and `HEAD /healthz`
- `GET` and `HEAD /readyz`

When configured with a status handler, it exposes:

- `GET` and `HEAD /admin/status`

When configured with a metrics handler, it exposes:

- `GET` and `HEAD /metrics`

When configured with the recovery handler, it exposes:

- `POST /admin/replay`
- `POST /admin/refinalize`

Unsupported verbs return `405 Method Not Allowed` with `Allow: GET, HEAD` for
probe and status endpoints, or `Allow: POST` for recovery endpoints.

## Current Runtime Shape

The shared admin surface is used by long-running runtimes such as API, MCP
server, ingester, reducer, and claim-driven collectors. `bootstrap-index` is a
one-shot helper and does not mount this runtime-local admin surface.

The public API admin surface remains under `/api/v0/admin/*` and is described
by `/api/v0/openapi.json`, not by this page.

## Probe Responses

`/healthz` and `/readyz` return text:

```text
service=<service> probe=<healthz|readyz> status=ok
```

Failed checks return HTTP 503 with `status=error` and the check error in the
body. `HEAD` returns only headers and status.

## Status Response

`/admin/status` supports:

- `format=text`
- `format=json`
- `Accept: application/json` when `format` is omitted

`HEAD /admin/status` follows the same format-selection rules and returns no
body.

The JSON report is rendered by `go/internal/status.RenderJSON` and may include:

- `version`
- `as_of`
- `health`
- `coordinator`
- `flow`
- `queue`
- `latest_failure`
- `retry_policies`
- `registry_collectors`
- `aws_cloud_scans`
- `aws_freshness`
- `vulnerability_sources`
- `aws_cloud_scans_truncated`
- `aws_cloud_scan_limit`
- `scope_activity`
- `generation_history`
- `generation_transitions`
- `scopes`
- `generations`
- `stages`
- `domains`
- `queue_blockages`
- `terraform_state`

Queue and domain age fields include both human-readable duration strings and
seconds values, such as `oldest_outstanding_age_seconds` and
`oldest_age_seconds`.

`vulnerability_sources` lists durable OSV, NVD, KEV, EPSS, or derived source
target state when the vulnerability intelligence collector has attempted a
target. Each row carries last attempt/success timestamps, next retry, last
error class, freshness state, terminal status, result count, warning count, and
the bounded collection window.

`terraform_state` carries bounded Terraform-state status. `last_serials` and
`recent_warnings` remain bounded admin evidence, and `warning_summary` groups
recent warnings by `warning_kind`, `reason`, public `scope_class`, and `count`
so release gates can reason about missing-state patterns without reading raw
facts.

## Recovery Routes

`POST /admin/replay` replays failed projector or reducer work through the Go
recovery handler. The request accepts `stage`, `scope_ids`, `failure_class`,
and `limit`. The response includes `status`, `stage`, `replayed`, and
`work_item_ids`.

`POST /admin/refinalize` re-enqueues projector work for `scope_ids`. The
response includes `status`, `enqueued`, and `scope_ids`.

Recovery routes are mounted only when the runtime is explicitly configured
with `WithRecoveryHandler`.

## Ownership

New runtimes should reuse the shared mux instead of creating bespoke probes or
status endpoints:

- `go/internal/runtime/admin.go`
- `go/internal/runtime/status_server.go`
- `go/internal/runtime/http_server.go`
- `go/internal/runtime/recovery_handler.go`
- `go/internal/status/http.go`
- `go/internal/status/json.go`

Runtime metrics are documented in [Telemetry Metrics](telemetry/metrics.md).
