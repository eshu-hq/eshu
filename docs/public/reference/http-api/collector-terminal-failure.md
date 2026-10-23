# Collector Terminal Failure Readiness

An instance whose collection failed terminally, for example because a provider
credential was revoked, must not read as healthy. The collector status routes
carry the failure as bounded fields.

## What reports it

Both routes read the newest unresolved, recent terminal work-item failure for
each enabled, coordinator-registered instance (see [When it clears](#when-it-clears)):

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

A terminal failure counts while both of these hold:

- It is recent: the `failed_terminal` work item was updated within
  3 x `scan_interval` of the instance, with a 30 minute floor. An instance
  without `scan_interval` uses the floor. A `scan_interval` the status reader
  cannot parse also uses the floor.
- It is unresolved: no later `completed` work item exists for the same
  instance and the same scope. A success on another scope, or by another
  instance that shares the scope id, does not clear it. A later pending,
  claimed, or retrying item does not clear it either; only a completed item
  proves recovery.

An instance that keeps failing fails again every cycle, so it stays reported.
A failure on a target that was removed from the configuration (an orphan) ages
out after the window. A retryable failure never sets the fields.

To clear it, fix the cause and let the next cycle complete the scope, or
remove the target and wait one window. There is no explicit acknowledge.

If the coordinator stops planning, a real failure also ages out after the
window. Check coordinator liveness and the work-item backlog before reading a
cleared failure as recovery. If items wait in the claim backlog for more than
about two scan intervals, a failing instance can age out between failures and
the signal can flap.

A disabled or deactivated instance is never read, so it keeps
`health=disabled` and reports no failure fields. A terminal failure on an
unregistered instance is ignored.

## Metric

`eshu_runtime_coordinator_collector_terminal_failure_instances` is a gauge of
affected instances by `collector_kind` and `failure_class`. Every runtime that
scrapes status exports the same value, so read it with
`max by (collector_kind, failure_class)`, never summed. See
[Metrics](../telemetry/metrics.md).

The query plan and the behavior proof for the read are in
`docs/internal/evidence/8803-collector-terminal-failure-readiness.md`.
