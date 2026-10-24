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

`last_failure_class` is a bounded class or `unknown`. Each collector kind may
report the classes that apply to it in the table below. Any persisted value
outside that kind's set reads `unknown`, including free text, an empty class,
and a class that only another kind owns, so a provider message never reaches
the field. A kind that is not first-party is hosted by the extension host: it
reports the common classes and the host's own, and any other class it defines
itself (a lowercase token of at most 96 characters) reads `extension_defined`,
so third-party classes never grow the set.

| Class | Kinds | Meaning |
| --- | --- | --- |
| `auth_denied` | every kind | The credential was refused or lacks the grant. |
| `not_found` | every kind | The target is missing or the credential cannot see it. |
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
| `invalid_claim` | extension host | The host rejected corrupt or unsupported claim input. |
| `invalid_result` | extension host | The host rejected the extension's output before commit. |
| `launch_failure` | extension host | The host could not launch the extension. |
| `status_record` | extension host | The host failed to record the extension's status. |
| `extension_defined` | extension host | The extension reported a class of its own. |

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
