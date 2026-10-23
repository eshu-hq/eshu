# Collector Terminal Failure Readiness

An instance whose collection failed terminally, for example because a provider
credential was revoked, must not read as healthy. The collector status routes
carry the failure as bounded fields.

## What reports it

Both routes read the newest terminal work-item failure for each enabled,
coordinator-registered instance:

- `GET /api/v0/status/collectors` (alias `/api/v0/collectors`) reports
  `health=degraded` and adds `last_failure_class`, `last_failure_operation`,
  `last_failure_terminal`, and `last_failure_at` to the instance row.
- `GET /api/v0/status/collector-readiness` (alias
  `/api/v0/collector-readiness`) reports `promotion_state=failed`, the blocker
  `terminal provider failure: <class> (<operation>)`, and
  `last_failure_class`, `last_failure_operation`, and `last_failure_at`.

The MCP tools `list_collectors` and `get_collector_readiness` return the same
fields. The status JSON and the plain-text status render carry them too. A
healthy instance omits all four fields.

## Values

`last_failure_class` is one of `auth_denied`, `not_found`, `rate_limited`,
`terminal`, `retryable`, or `unknown`. Any persisted value outside that set
reads as `unknown`. `last_failure_operation` is a short lowercase token such as
`list_org_alerts`, or `unknown`.

The failure message and the provider status code are never returned. A caller
that needs the cause reads the collector log line carrying `failure_class`.

## When it clears

The failure is unresolved when the newest `failed_terminal` work item of the
instance has no later `completed` work item for the same scope. The next
successful cycle for that scope clears it with no operator action. A success on a
different scope does not clear it. A retryable failure never sets it.

A disabled or deactivated instance keeps `health=disabled` and reports no
degraded health. A terminal failure on an unregistered instance is ignored.

## Metric

`eshu_runtime_coordinator_collector_terminal_failure_instances` is a gauge of
affected instances by `collector_kind` and `failure_class`. See
[Metrics](../telemetry/metrics.md).

The query plan and the behavior proof for the read are in
`docs/internal/evidence/8803-collector-terminal-failure-readiness.md`.
