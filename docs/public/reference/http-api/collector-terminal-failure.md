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
they keep the class, operation, blocker, and `acknowledged_failure`, and only
`instance_id` is withheld. The operator's acknowledgement reason is on no status
surface. Callers on the shared key and browser sessions get the full rows;
every scoped-token caller gets the redacted view, including an all-scope
scoped token.

## Values

`last_failure_class` is a bounded class or `unknown`. Each collector kind may
report the classes that apply to it in the table below. On an in-tree instance
of a first-party kind, any persisted value outside that kind's set reads
`unknown`, including free text, an empty class, and a class that only another
kind owns. On an extension-hosted instance, any other pattern-matching class
reads `extension_defined`. Either way a provider message never reaches the
field.

Every kind reports the extension host's own classes (`invalid_claim`,
`invalid_result`, `launch_failure`, `partial_snapshot_withheld`, `source_state_record`, `status_record`),
because a component extension can be configured with a first-party kind such
as `jira` and the host then writes those classes for it.

An instance is extension-hosted when the generic component planner claims its
stored configuration (the same check the coordinator uses to hand an instance
to the extension host, which includes a malformed or non-object configuration),
or when its kind is not first-party, because only the
extension host runs a kind the platform does not ship. On such an instance any
other class it defines itself (a lowercase token of at most 96 characters)
reads `extension_defined`, so third-party classes never grow the set, whether
the instance carries a first-party kind such as `jira` or a kind of its own. An
in-tree instance of a first-party kind never collapses: a class an in-tree
writer forgot to register stays `unknown`, so the writer tie test keeps
catching it. The marker is a boolean the status reader derives from existing
state; it adds no field to the payload and no label value to the gauge.

Every surface that reports a `failure_class` for an instance bounds it by that
instance, so they agree: collector status and readiness, the failed-scope list,
the acknowledge response, and the stored acknowledgement records. A stored
acknowledgement record reads its instance's current configuration, not the
configuration at acknowledgement time, so reconfiguring an instance (for
example from a component activation to `{}`) changes how its old records
read. A record whose instance is no longer registered has no configuration to
read, so it reads the kind-only form (`unknown` for a class only an extension
defines).

Known limit, reading `unknown` rather than leaking text:

- `legacy_missing_generation_identity` reads `unknown` on an in-tree instance of
  a first-party kind. On an extension-hosted instance or any other kind it
  matches the extension host class pattern and reads `extension_defined`. The
  schema bootstrap backfill writes it once, for every kind, on pending or
  claimed work items that predate generation identity, and it ages out with
  the window below.

| Class | Kinds | Meaning |
| --- | --- | --- |
| `auth_denied` | every kind | The credential was refused or lacks the grant. |
| `not_found` | every kind | The target is missing or the credential cannot see it. For `security_alert` it also covers a repository whose Dependabot alerts are disabled (a 403, #8947). |
| `rate_limited` | every kind | The provider throttled the run. |
| `terminal` | every kind | The provider returned another non-retryable error. |
| `retryable` | every kind | A transient provider failure used up the attempt budget. |
| `attempt_budget_exhausted` | every kind | The attempt budget ran out on a cause that carried no class. |
| `identity_mismatch` | every kind | The collected generation's identity did not match the claim. |
| `collect_failure` | every kind | A non-retryable collect error carried no class. |
| `commit_failure` | every kind | A non-retryable commit error carried no class. |
| `permission_hidden` | `jira` | Jira refused the credential (401 or 403) or hid the data behind a permission. |
| `deleted` | `jira` | The Jira resource was not found (404). |
| `archived` | `jira` | The Jira resource is gone or archived (410). |
| `permission_denied` | `aws` | AWS rejected a service scan as access denied or unauthorized; fix the IAM permissions. |
| `unsupported_permission` | `aws` | AWS rejected a top-level metadata operation as unsupported for the scope. |
| `stale_fence` | `aws` | The scan-status store rejected the claim as stale, so the claim stopped retrying. |
| `registry_auth_denied` | `oci_registry`, `package_registry`, `sbom_attestation` | The registry refused the credential (401 or 403). |
| `registry_not_found` | `oci_registry`, `package_registry`, `sbom_attestation` | The registry repository, package, or document is missing (404). |
| `registry_rate_limited` | `oci_registry`, `package_registry`, `sbom_attestation` | The registry throttled the run and the attempt budget ran out. |
| `registry_retryable_failure` | `oci_registry`, `package_registry`, `sbom_attestation` | A transient registry or transport failure used up the attempt budget. |
| `registry_canceled` | `oci_registry`, `package_registry`, `sbom_attestation` | The collector was canceled before it finished. |
| `registry_terminal_failure` | `oci_registry`, `package_registry`, `sbom_attestation` | The registry returned another non-retryable status. |
| `registry_metadata_too_large` | `package_registry` | Package metadata exceeded the configured byte limit. |
| `non_retryable` | `vulnerability_intelligence` | The vulnerability source returned a non-retryable 4xx. |
| `analyzer_failed` | `scanner_worker` | The analyzer process or tool failed. |
| `cpu_limit_exceeded` | `scanner_worker` | The analyzer ran out of its CPU budget. |
| `memory_limit_exceeded` | `scanner_worker` | The analyzer ran out of its memory budget. |
| `timeout` | `scanner_worker` | The analyzer timed out. |
| `input_limit_exceeded` | `scanner_worker` | The input payload was larger than the limit. |
| `file_limit_exceeded` | `scanner_worker` | The target held more files than the limit. |
| `fact_limit_exceeded` | `scanner_worker` | The analyzer produced more facts than the limit. |
| `target_unavailable` | `scanner_worker` | The bounded target was missing or unreadable. |
| `unsupported_target` | `scanner_worker` | The target shape is one this worker cannot scan. |
| `source_unavailable` | `scanner_worker` | A source dependency of the scan was unavailable. |
| `commit_failed` | `scanner_worker` | Persisting the produced facts failed. |
| `waiting_on_git_generation` | `terraform_state` | The scan kept waiting for a Git generation until the attempt budget ran out. |
| `invalid_claim` | every kind, from the extension host | The host rejected corrupt or unsupported claim input. |
| `invalid_result` | every kind, from the extension host | The host rejected the extension's output before commit. |
| `launch_failure` | every kind, from the extension host | The host could not launch the extension. |
| `source_state_record` | every kind, from the extension host | The host could not record a completed claim's source-state attempt row (#9420); the claim fails retryable. |
| `partial_snapshot_withheld` | every kind, from the extension host | The host withheld incomplete evidence before fact commit and scheduled a retry; previous active facts remain. |
| `status_record` | every kind, from the extension host | The host failed to record the extension's status. |
| `extension_defined` | extension-hosted instances | The extension reported a class of its own. |

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
remove the target and wait one window. If the failure is expected and nobody
will fix it, [acknowledge it](#acknowledge-a-failure). An acknowledgement
replaces the failed state with `partial`; it does not make the instance healthy.

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

## Acknowledge a failure

A terminal failure on a target nobody will fix, such as a repository deleted
upstream, a credential revoked on purpose, or a target you removed and do not
want to wait a window for, keeps the instance `degraded` and `failed` for as
long as the target keeps failing. Acknowledge that failure to say it is known and
expected. If you intend to fix it, do not acknowledge: fix the cause and let a
completed work item clear it.

```text
POST /api/v0/admin/collector-failures/acknowledge
{"collector_kind": "...", "instance_id": "...", "scope_id": "...",
 "reason": "...", "idempotency_key": "..."}
```

```bash
eshu admin collector-failure acknowledge --collector-kind security_alert \
  --instance ID --scope SCOPE --reason "repository deleted upstream"
```

The CLI generates an idempotency key and prints it to stderr before the call when you
do not pass one. The acknowledgement names the newest unresolved failure of the scope
at the time of the call and stores its observed raw class.

Find the instance and its kind on `GET /api/v0/status/collector-readiness` or with
the `get_collector_readiness` MCP tool, then [find the failed scopes](#find-the-failed-scopes).
The acknowledge call needs an admin (all-scopes) token; a scoped token receives `403`.

### Find the failed scopes

No status surface carries a scope id: a scope id names an org, repository, or
account, and the status payloads reach callers outside the admin gate (browser
sessions, shared keys, and the MCP tools). The scope ids come from an admin-only
read of one instance:

```text
POST /api/v0/admin/collector-failures/query
{"collector_kind": "security_alert", "instance_id": "...", "limit": 100, "cursor": "..."}
```

```bash
eshu admin collector-failure list --collector-kind security_alert --instance ID
```

Each item is a scope with its newest unresolved failure inside the instance's
window: `scope_id`, `failure_class`, `failure_operation`, `failed_at`,
`work_item_id`, `acknowledged`, and `acknowledged_at` when acknowledged. The list
is exactly what collector status counts: the same window, the same rule that a later
completed work item resolves a failure, and the same acknowledgement cover. A scope
that recovered or aged out is not listed, and a scope with an acknowledged older
failure and a newer failure of another class lists both. Every unacknowledged
scope it lists is one the acknowledge call accepts.

`limit` counts scopes (default 100, at most 500) and `cursor` is the `next_cursor`
of the previous page, returned when `truncated` is true. Items are ordered by
`scope_id`, unacknowledged before acknowledged. The response never carries a
failure message, a provider status code, or an acknowledgement reason; it needs an
admin (all-scopes) token, so a scoped token or a browser session without all scopes
receives `403`. An unknown, mismatched, disabled, or deactivated instance lists
nothing. Reading it writes no audit event.

### What an acknowledgement does

- The instance stops failing. An instance whose only unresolved failures are
  acknowledged reads `health=partial` and `promotion_state=partial`, with the
  blocker `terminal fetch failure acknowledged: <class> (<operation>)`, never
  `healthy` or `implemented`, and never `failed`. The failure is still happening,
  so no surface reads it as healthy.
- The failure stays visible as a separate `acknowledged_failure` object on
  `GET /api/v0/status/collectors` and `GET /api/v0/status/collector-readiness`:
  `failure_class`, `failure_operation`, `failed_at`, and `acknowledged_at`.
  `last_failure_*` describes only unacknowledged failures. The readiness next
  action names the conditions below.
- A real unacknowledged failure on another scope of the same instance still
  degrades it. The status read returns the unacknowledged failure and the
  acknowledged one separately, so an acknowledgement never hides another scope.

### When it is reported again

An acknowledgement records the newest unresolved failure of the scope: its
`updated_at`, its raw `last_failure_class` (NULL and empty agree), and its work
item id. A failed work item `t` on that instance and scope is covered while

- `t.updated_at` is not later than the acknowledged failure, or
- `t` has the same raw class and no completed work item of the target landed
  between the acknowledged failure and `t`.

So the failure is reported again, with no further write, when

1. a completed work item lands on the scope after the acknowledged failure and a
   later failure follows (the scope recovered, then failed again), or
2. a later failure carries a different raw class (a different cause, to which your
   reason may not apply).

A target that keeps failing with the same class every cycle stays acknowledged.

Limits:

- The acknowledgement has no expiry. A scope re-added later that fails with the
  same class and has no success in between stays acknowledged, and still reads
  `partial`, not healthy.
- The class compare is on the raw persisted token. A class that is free text
  outside the bounded vocabulary (it already reads `unknown`) and varies per
  cycle lapses the acknowledgement.
- A removed or deactivated instance leaves an inert acknowledgement row. Rows are
  bounded by the number of (instance, scope) pairs ever acknowledged.
- Retention (workflow work-item retention) may delete the acknowledged work item;
  the acknowledgement keeps the failure time and class, so later same-class
  failures stay covered.
- One call acknowledges one scope. An instance whose every target failed needs
  one call per scope.

### Request rules

- `reason` (at most 512 characters) and `idempotency_key` are required. The
  reason is stored as typed. It is returned only by
  `POST /api/v0/admin/collector-failures/acknowledgements/query` (admin token);
  no status route, MCP tool, metric, or audit event carries it. Do not put
  secrets in it.
- The failure must be inside the instance's window (3 x `scan_interval`, 30
  minute floor) with no later completed work item. Otherwise the call answers
  `422` with `ack_refused_no_terminal_failure` and writes nothing. A refusal
  before the ledger claim leaves the key unconsumed. The same refusal from the
  re-resolve inside the acknowledgement transaction (a resolution landed after
  the first check) consumes the key: a retry with the same key answers `409`
  until a fresh `idempotency_key` is used, and the `detail` in the `422` body
  says which case applied. An unknown, disabled, or deactivated instance, or a
  kind that does not match, answers `404`. The same `404` from the re-resolve
  inside the acknowledgement transaction (the instance was disabled or
  deactivated after the first check) also consumes the key. Its body keeps the
  same `error` and `detail` and adds `key_consumed: true` and a `key_detail`
  that says nothing was acknowledged and a retry needs a new `idempotency_key`.
- The same key with the same instance and scope replays the stored outcome with
  `status=duplicate`; the same key with another target, or a key whose call is
  still in progress, answers `409`. Two different keys on one scope both commit
  and the later row stands; both are audited.
- Each decision appends one `admin_recovery_action` governance audit event with a
  bounded reason code (`ack_recorded`, `ack_idempotent_replay`,
  `ack_refused_missing_reason`, `ack_refused_missing_idempotency_key`,
  `ack_refused_invalid_request`, `ack_refused_unauthorized`,
  `ack_refused_instance_not_found`, `ack_refused_no_terminal_failure`,
  `ack_idempotency_key_reused`, `ack_failed`) and the hashed actor.
- An acknowledgement deletes nothing. The failed work items stay for triage until
  retention removes them.

Acknowledging is not fixing. Check coordinator liveness first: a failure that
disappears because the coordinator stopped planning is not an expected failure.
The gauge
`eshu_runtime_coordinator_collector_terminal_failure_acknowledged_instances`
keeps counting acknowledged instances by kind and class, so a failure nobody
fixed stays visible on a dashboard without paging.

## Metric

`eshu_runtime_coordinator_collector_terminal_failure_instances` is a gauge of
affected instances by `collector_kind` and `failure_class`. It counts only
unacknowledged failures; `eshu_runtime_coordinator_collector_terminal_failure_acknowledged_instances`
counts acknowledged ones with the same labels, and
`eshu_dp_collector_terminal_failure_acknowledgements_total{outcome}` counts
acknowledgement requests (`acknowledged`, `duplicate`, `refused`). Every runtime that
scrapes status exports the same value, so read it with
`max by (collector_kind, failure_class)`, never summed. See
[Metrics](../telemetry/metrics.md).

The query plan and the behavior proof for the read are in
`docs/internal/evidence/8803-collector-terminal-failure-readiness.md`.
