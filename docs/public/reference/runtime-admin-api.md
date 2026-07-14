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
- `POST /admin/replay-collector-generations`

Unsupported verbs return `405 Method Not Allowed` with `Allow: GET, HEAD` for
probe and status endpoints, or `Allow: POST` for recovery endpoints.

## Current Runtime Shape

The shared admin surface is used by long-running runtimes such as API, MCP
server, ingester, reducer, and claim-driven collectors. `bootstrap-index` is a
one-shot helper and does not mount this runtime-local admin surface.

The public API admin surface remains under `/api/v0/admin/*` and is described
by `/api/v0/openapi.json`, not by this page.

## Public Admin Dead-Letter Read

The public query API exposes a first-class dead-letter list so operators and
component-extension authors do not need hand-written SQL against
`fact_work_items`:

- `POST /api/v0/admin/dead-letters/query`
- MCP mirror: `list_dead_letter_work_items`

The request requires both `limit` (`1` to `500`) and `timeout_ms` (`1` to
`30000`). Optional filters are `failure_class`, `domain`, `scope_id`,
`collector_kind`, `updated_after`, and `updated_before`. Time filters are
RFC3339 and apply to `fact_work_items.updated_at`. Results are ordered
deterministically by `updated_at DESC, work_item_id ASC` and return
`truncated=true` when more rows matched than the requested limit.

Scoped component-extension tokens are restricted to their granted repository or
scope IDs before rows are read. The response includes work item IDs, scope IDs,
generation IDs, stage, domain, collector kind, failure class, attempt count,
and timestamps for the caller's visible rows, but it does not expose raw
failure messages or fact payloads.

Runbook: find dead letters with `POST /api/v0/admin/dead-letters/query` or
`list_dead_letter_work_items`, group by `failure_class`, fix the root cause, then
use targeted `/api/v0/admin/replay` only for rows whose class is safe to replay.

## Public Admin Input-Invalid-Fact Quarantine Read

Issue #4630 adds a durable per-fact quarantine read surface. When the reducer
decodes a typed fact payload and finds a required field missing or null, it
skips that one fact (the rest of the batch still projects), records a visible
`eshu_dp_reducer_input_invalid_facts_total` counter increment plus a structured
error log, and best-effort persists a durable row to
`reducer_input_invalid_facts` so an operator can query it after the fact
instead of only seeing an aggregate rate or a single log line:

- `POST /api/v0/admin/input-invalid-facts/query`
- MCP mirror: `list_reducer_input_invalid_facts`

The request requires `scope_id`, `generation_id`, `limit` (`1` to `500`), and
`timeout_ms` (`1` to `30000`). Optional filters are `domain` and `fact_kind`.
Results are ordered deterministically by
`decided_at DESC, fact_id ASC, missing_field ASC` and return `truncated=true`
when more rows matched than the requested limit.

Scoped component-extension tokens are restricted to their granted repository or
scope IDs before rows are read. The response includes `fact_id`, `fact_kind`,
`missing_field`, `failure_class`, `domain`, `scope_id`, `generation_id`, and
`decided_at` for the caller's visible rows — never the raw fact payload.

The durable write is best-effort and idempotent: a write failure is logged and
counted (`eshu_dp_reducer_input_invalid_fact_write_errors_total`) but never
fails the owning reducer intent, and replaying the same quarantine (a retried
intent, or a re-projected generation) converges on one row per
`(scope_id, generation_id, fact_id, missing_field)` rather than duplicating it.
"Recompute on demand" for this surface means reading these persisted rows,
populated during the reduction that quarantined them — re-driving requires
re-running the collector/reducer after the collector defect is fixed, not a
live re-decode in the query path.

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
- `collector_runtimes`
- `flow`
- `queue`
- `latest_failure`
- `retry_policies`
- `registry_collectors`
- `aws_cloud_scans`
- `aws_freshness`
- `vulnerability_sources`
- `semantic_extraction`
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
- `collector_generation_dead_letters`
- `terraform_state`

`collector_runtimes` is a derived, additive view over coordinator registration,
direct collector status evidence, and active persisted source or reducer fact
evidence. It classifies rows as
`coordinator_managed`, `direct_mode`, `disabled`, `unregistered`, or
`profile_gated` when a profile gate emits an explicit status row. Rows include
the collector instance ID, kind, runtime mode, coordinator registration flag,
evidence sources, bounded `source_systems`, health label, and timestamps when
available. Source-specific documentation collectors preserve the neutral
`collector_kind=documentation` fact model while exposing identities such as
`source_systems=["confluence"]` for operator readback.

Queue and domain age fields include both human-readable duration strings and
seconds values, such as `oldest_outstanding_age_seconds` and
`oldest_age_seconds`.

`collector_generation_dead_letters` reports collector commit failures that
happened before normal projector work items existed. It includes
`dead_letter`, `replay_requested`, `replay_attempts`,
`oldest_dead_letter_age`, and `oldest_dead_letter_age_seconds` for unresolved
rows. It does not include fact payloads, repository names, local paths,
credential handles, or provider response bodies.

`coordinator.collector_backpressure` reports bounded claim-aware collector
pressure by collector kind, collector instance, and source system. It includes
pending, claimed, retrying, terminal, expired, active-claim, overdue-claim, and
collector-generation dead-letter counts plus oldest pending, retry, and claim
ages. Failure classes are aggregate counts only. It does not include provider
URLs, scope ids, generation ids, source locators, credential handles, resource
identifiers, payload excerpts, or raw failure messages.

`vulnerability_sources` lists durable OSV, NVD, KEV, EPSS, or derived source
target state when the vulnerability intelligence collector has attempted a
target. Each row carries last attempt/success timestamps, next retry, last
error class, freshness state, terminal status, result count, warning count, and
the bounded collection window.

`semantic_extraction` reports optional LLM-assisted semantic extraction status.
When no provider is configured it returns `state=unavailable`,
`reason=provider_not_configured`, and both documentation observations and code
hints disabled. This status is informational and does not mark health or
readiness unhealthy; deterministic collectors, parser output, reducer
projection, API reads, MCP tools, and docs verification remain unaffected.
For the full no-provider, provider-profile, policy, and security model, see
[Semantic Enrichment Posture](semantic-enrichment-posture.md).

When `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` defines hosted provider profiles,
the same section includes `provider_profiles[]` with redacted profile id,
provider kind, model id, endpoint profile id, credential source kind, source
classes, credential/source-policy booleans, and profile state. Credential source
handles and raw keys are never rendered. Provider traffic, credential loading,
prompt retention, and provider response retention remain out of scope for this
status surface; source policy must enable a profile's source class before
documentation observations or code hints report enabled.

`terraform_state` carries bounded Terraform-state status. `last_serials` and
`recent_warnings` remain bounded admin evidence; recent warning rows include
public-safe `source_handle` and `safe_locator_hash` fields for source-level
triage. `warning_summary` groups recent warnings by `warning_kind`, `reason`,
public `scope_class`, and `count` so release gates can reason about
missing-state patterns without reading raw facts.

## Recovery Routes

`POST /admin/replay` replays failed projector or reducer work through the Go
recovery handler. The request accepts `stage`, `scope_ids`, `failure_class`,
and `limit`. The response includes `status`, `stage`, `replayed`, and
`work_item_ids`.

`POST /admin/refinalize` re-enqueues projector work for `scope_ids`. The
response includes `status`, `enqueued`, and `scope_ids`.

`POST /admin/replay-collector-generations` marks collector generation commit
failures for source-level replay. The request accepts required
`collector_kind`, optional `scope_ids`, optional `failure_class`, and `limit`.
The response includes `status`, `replayed`, and `generation_ids`. This route
does not reconstruct a consumed fact stream; it records a bounded replay request
after the underlying commit failure has been fixed. A later successful commit
for the same source scope marks unresolved rows `replayed`.

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
