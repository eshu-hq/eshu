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
  `terminal fetch failure: <class> (<operation>)`, and
  `last_failure_class`, `last_failure_operation`, and `last_failure_at`.

The MCP tools `list_collectors` and `get_collector_readiness` return the same
fields. The status JSON and the plain-text status render carry them too. The
operator control plane (`get_operator_control_plane`) rolls the failed instance
up, so its collector family reads `promotion_state=failed` with the same
blocker. A healthy instance omits the failure fields on both routes.

Scoped callers receive only the per-kind health aggregate on the collectors
route; `last_failure_*` fields are withheld from them. On the readiness route
they keep the class, operation, and blocker, and only `instance_id` is
withheld. Callers on the shared key and browser sessions get the full rows;
every scoped-token caller gets the redacted view, including an all-scope
scoped token.

## Values

`last_failure_class` is one of these, or `unknown` for any persisted value
outside the set, including the bounded classes other collector kinds write for
themselves (for example `registry_auth_denied`, `permission_denied`,
`stale_fence`); health and the blocker are still reported for those kinds,
only the class reads `unknown` (#8896):

| Class | Written by | Meaning |
| --- | --- | --- |
| `auth_denied` | security-alert provider | The credential was refused or lacks the grant. |
| `not_found` | security-alert provider | The target is missing or the credential cannot see it. |
| `rate_limited` | security-alert provider | The provider throttled the run. |
| `terminal` | security-alert provider | The provider returned another non-retryable error. |
| `retryable` | security-alert provider | A transient provider failure used up the attempt budget. |
| `attempt_budget_exhausted` | claim runner | The attempt budget ran out on a cause that carried no class. |
| `identity_mismatch` | claim runner | The collected generation's identity did not match the claim. |
| `collect_failure` | claim runner | A non-retryable collect error carried no class. |
| `commit_failure` | claim runner | A non-retryable commit error carried no class. |

`last_failure_operation` is a short lowercase token such as `list_org_alerts`,
`collect_failure`, or `commit_failure`, or `unknown`.

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
out after the window. A failure that is still being retried never sets the
fields: only a `failed_terminal` work item counts. A transient failure that used
up the attempt budget does land as `failed_terminal`, and then keeps the class
`retryable` its error carried. The
`failed_terminal` status decides what counts: a dead-lettered item counts even
when its collector did not mark the failure terminal, as the scanner worker
does not.

To clear it, fix the cause and let the next cycle complete the scope, or
remove the target and wait one window. There is no explicit acknowledge.

If the coordinator stops planning, a real failure also ages out after the
window. Check coordinator liveness and the work-item backlog before reading a
cleared failure as recovery. If items wait in the claim backlog for more than
about two scan intervals, a failing instance can age out between failures and
the signal can flap.

Known limits of the window:

- The window uses the instance's `scan_interval`, not the coordinator's global
  `ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL`, which the status reader does
  not know. With no `scan_interval` set and a global reconcile interval longer
  than 10 minutes, a failing instance can age out between failures and the
  signal can flap. Set `scan_interval` on the instance to match.
- A bootstrap instance is always planned on the global reconcile interval, and
  the coordinator ignores its `scan_interval`. The status window still uses
  that `scan_interval`, so a bootstrap failure that no later run revisits can
  stay reported for 3 x that interval, or the 30 minute floor when that is
  longer.

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
