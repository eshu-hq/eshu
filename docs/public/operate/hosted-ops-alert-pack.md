# Hosted Ops Alert Pack

Use the hosted ops alert pack when operating a Kubernetes-hosted Eshu deployment
after remote Compose and Helm rollout proof have passed. It keeps dashboard
health, queue convergence, dependency readiness, and answer completeness visible
without exposing source-specific data.

## Assets

| Asset | Purpose |
| --- | --- |
| `grafana/dashboards/eshu-hosted-operations.json` in the eshu-deploy checkout | Grafana dashboard for hosted API, MCP, ingester, resolution engine, workflow coordinator, and collector operations. |
| `observability/hosted-operations-alerts.yaml` in the eshu-deploy checkout | Standalone Prometheus rules for environments that load rule files directly. |
| `observability/hosted-operations-prometheus-rule.yaml` in the eshu-deploy checkout | Prometheus Operator `PrometheusRule` wrapper carrying the same alert intent. |
| `$ESHU_DEPLOY_CHECKOUT/scripts/verify-hosted-ops-alert-pack.sh` | Static verifier for dashboard JSON, alert names, runbook annotations, bounded labels, and Helm ServiceMonitor render shape. |
| `$ESHU_DEPLOY_CHECKOUT/scripts/test-verify-hosted-ops-alert-pack.sh` | Mutation harness that proves the verifier fails closed on missing panels, missing alerts, and missing runbook annotations. |

## Dashboard Contract

The dashboard must show these bounded signals:

- hosted runtime health by `service_name` and health state;
- queue pending, retrying, failed, dead-letter, and oldest outstanding age;
- API HTTP 5xx rate and MCP tool error rate;
- Postgres query, graph query, and canonical write p99 latency;
- workflow coordinator overdue claims and oldest pending claim age;
- generation pending and failed counts for completeness drift;
- schema bootstrap Job failures;
- a debug posture panel that reminds operators to prove pprof, logs, and
  bounded readback before changing rollout or worker settings.

Panels must use bounded labels such as `service_name`, `service_namespace`,
`state`, and `collector_kind`. Do not add repository, path, source payload,
token, account, or work-item labels to dashboard queries.

## Alert Contract

The alert pack covers:

- missing hosted runtime metrics;
- dead-lettered queue work;
- sustained queue age;
- API/MCP readback completeness drift;
- stalled collector claims;
- Postgres, graph, and canonical write latency;
- degraded or stalled runtime dependency health;
- schema bootstrap failure;
- MCP tool error rate;
- status summary freshness (see below).

Every alert must include `severity`, `component`, `runbook_section`, and a
`runbook` annotation that points at a concrete diagnostic check. Alert text must
not tell operators to restart broadly. It should identify the owning runtime,
status surface, metric family, or rollout proof to inspect next.

## Status Summary Alerts

Two alerts watch the status read path (#7009). They use only metrics the code
defines, and `$ESHU_DEPLOY_CHECKOUT/scripts/verify-hosted-ops-alert-pack.sh` fails if a metric they
name is not defined as a quoted name in `go/`.

| Alert | Fires when | Severity |
| --- | --- | --- |
| `EshuHostedStatusSummaryStale` | `eshu_runtime_status_summary_stale` is `1` for 5 minutes: the scrape could not serve a fresh stored row for a `model_key` (`active_work_summary` or `coordinator_snapshot`). The runtime compares the row age with its own `ESHU_STATUS_SUMMARY_STALE_AFTER`, so the rule holds no copy of the bound. A stopped or failing writer shows here as a row that keeps aging. | warning |
| `EshuHostedStatusSnapshotUnavailable` | `eshu_runtime_status_snapshot_available` is `0` for 5 minutes. Status-derived gauges are omitted, so the queue and completeness alerts above cannot fire. | critical |

`EshuHostedStatusSummaryStale` is inert until `ESHU_STATUS_SUMMARY_READ_ENABLED`
is on, because the stale gauge is rendered only then. A writer that is disabled
while the reader is on also shows as stale. There is no separate writer alert:
`eshu_dp_status_summary_writer_up` is set to `0` only when the writer loop
returns, which happens when the reducer is shutting down, and the series then
disappears with the process.

`EshuHostedStatusSnapshotUnavailable` is active on default deployments. It fires
whenever the status snapshot read fails or exceeds the scrape's 2 second status
read timeout, which includes the live active-work statement when the reader flag
is off. If it fires, treat the stale alert as unreadable: a failed snapshot also
drops the stale gauge.

## Drift Audit Correctness Alerts

One alert watches the incremental drift audit denominator (#11032, Part of
#10860). It uses only metrics the reducer registers, and the deploy-side
verifier fails if a metric it names is not defined as a quoted name in `go/`.

| Alert | Fires when | Severity |
| --- | --- | --- |
| `EshuAWSDriftConsecutiveCorrectingAudits` | `eshu_dp_aws_drift_incremental_audits_total{outcome="correcting"}` increases in both the last 25h window and the preceding 25h window: two consecutive daily `aws_cloud_runtime_drift` audits each found keys the delta run would have missed. The 25h windows are the 24h audit interval plus stagger slack; the rule holds no copy of the interval. The alert needs ~50h of post-deploy history before it can first fire. Two different scopes each correcting once in adjacent windows also fires: that is the intended domain-level unit (systematic delta blindness), not a per-scope claim. | critical |

Signal: the `correcting` outcome of `eshu_dp_aws_drift_incremental_audits_total`
(the denominator; `clean` means the replay agreed, `failed` means the
reconstruction errored). Magnitude comes from
`eshu_dp_aws_drift_incremental_audit_corrections_total`. Threshold: correcting
audits on two consecutive daily audits. Destination: this hosted ops alert pack
(`observability/hosted-operations-alerts.yaml` plus the PrometheusRule wrapper
in the eshu-deploy checkout), routed by Alertmanager `severity: critical`
paging; no issue is filed anywhere. When it fires, inspect the delta
filter/diff path the audit replays (the config-anchor moves the audit exists
to correct), not the audit itself; never restart broadly to clear it.

No-Regression Evidence: baseline is the audit path without the denominator
(one corrections point only when misses exist); after is one additional
`audits_total` increment per `audit_due` reconstruction at the 24h audit
cadence. Backend: in-process OTEL meter only (no Postgres, no new DB write,
no new table or migration). Input shape: a single audit reconstruction;
terminal row counts: exactly one point per audit (`clean`, `correcting`, or
`failed`). Telemetry evidence: handler tests pin one-point-per-audit semantics
and delta/skip/fallback silence. Safe because the emit is observability-only
(never fails the run), runs only on the daily audit path (never delta, skip,
or fallback), and touches no writer, fence, retire, lock, or queue path.

Observability Evidence: the new `eshu_dp_aws_drift_incremental_audits_total`
counter (closed outcomes `clean|correcting|failed`, bounded labels) plus this
alert rule and runbook; reducer telemetry coverage row updated in
`telemetry-coverage-reducer-core.md`.

## Health Versus Completeness

Process health is not answer readiness. `/healthz`, `/readyz`, and Kubernetes
rollout state prove that a runtime is alive and its declared dependencies are
reachable. Completeness requires queue state, `/admin/status`,
`/api/v0/index-status`, and a bounded API or MCP read that proves indexed state
is current enough for the intended question.

Use alerts in this order during incidents:

1. Restore telemetry if `EshuHostedMetricsMissing` fires.
2. Check runtime dependency health and schema bootstrap state.
3. Check queue dead letters, oldest age, retries, and overdue claims.
4. Compare API/MCP readback and index completeness.
5. Use pprof or traces only after the owning runtime and stage are known.

## Verification

Run the focused gate before publishing alert or dashboard changes:

```bash
"$ESHU_DEPLOY_CHECKOUT/scripts/test-verify-hosted-ops-alert-pack.sh"
"$ESHU_DEPLOY_CHECKOUT/scripts/verify-hosted-ops-alert-pack.sh"
helm lint "$ESHU_DEPLOY_CHECKOUT/helm/eshu"
```

Deploy assets live in the eshu-deploy checkout (`$ESHU_DEPLOY_CHECKOUT`).

Docs or navigation edits also require:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```
