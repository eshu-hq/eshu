# Hosted Ops Alert Pack

Use the hosted ops alert pack when operating a Kubernetes-hosted Eshu deployment
after remote Compose and Helm rollout proof have passed. It keeps dashboard
health, queue convergence, dependency readiness, and answer completeness visible
without exposing source-specific data.

## Assets

| Asset | Purpose |
| --- | --- |
| `deploy/grafana/dashboards/eshu-hosted-operations.json` | Grafana dashboard for hosted API, MCP, ingester, resolution engine, workflow coordinator, and collector operations. |
| `deploy/observability/hosted-operations-alerts.yaml` | Standalone Prometheus rules for environments that load rule files directly. |
| `deploy/observability/hosted-operations-prometheus-rule.yaml` | Prometheus Operator `PrometheusRule` wrapper carrying the same alert intent. |
| `scripts/verify-hosted-ops-alert-pack.sh` | Static verifier for dashboard JSON, alert names, runbook annotations, bounded labels, and Helm ServiceMonitor render shape. |
| `scripts/test-verify-hosted-ops-alert-pack.sh` | Mutation harness that proves the verifier fails closed on missing panels, missing alerts, and missing runbook annotations. |

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
- MCP tool error rate.

Every alert must include `severity`, `component`, `runbook_section`, and a
`runbook` annotation that points at a concrete diagnostic check. Alert text must
not tell operators to restart broadly. It should identify the owning runtime,
status surface, metric family, or rollout proof to inspect next.

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
scripts/test-verify-hosted-ops-alert-pack.sh
scripts/verify-hosted-ops-alert-pack.sh
helm lint deploy/helm/eshu
```

Docs or navigation edits also require:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```
