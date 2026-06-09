# Observability Deployment Assets

## Purpose

`deploy/observability` holds optional local and Kubernetes observability assets:
Prometheus alert rules and a small OpenTelemetry Collector config.

## Ownership Boundary

These files package alert and collector wiring. They do not define metric
contracts, runtime health semantics, or operator runbooks. Metric names live in
`go/internal/telemetry`; public guidance lives under `docs/public/reference/telemetry`.

## Surface

- `alerts.yaml` contains standalone Prometheus rule groups, including the
  `eshu.freshness` group for generation convergence and trigger handoff.
- `prometheus-rule.yaml` wraps the same alert intent for Prometheus Operator
  environments and carries the same `eshu.freshness` group.
- `hosted-operations-alerts.yaml` contains standalone hosted ops rules for
  runtime metrics, queue convergence, dependency health, collector claims,
  schema bootstrap, and MCP tool errors.
- `hosted-operations-prometheus-rule.yaml` wraps the hosted ops rules for
  Prometheus Operator environments.
- `otel-collector-config.yaml` receives OTLP, batches telemetry, exports traces
  to Jaeger, and exposes Prometheus metrics.
- The freshness/convergence dashboard lives at
  `../grafana/dashboards/eshu-freshness-convergence.json`; its panel-to-metric
  map is documented in `docs/public/operate/freshness-convergence.md`.
- The hosted operations dashboard lives at
  `../grafana/dashboards/eshu-hosted-operations.json`; its alert and dashboard
  contract is documented in `docs/public/operate/hosted-ops-alert-pack.md`.

## Gotchas / Invariants

- Alert expressions must use bounded labels only.
- Keep standalone Prometheus and `PrometheusRule` intent aligned.
- Change thresholds in rule files, not prose.
- Some metric names still carry legacy `neo4j` wording. Do not rename them
  without a telemetry compatibility plan.

## Verification

After changing this directory, validate YAML with the target deployment tool,
confirm rule groups load, and check referenced metrics against
`go/internal/telemetry/instruments.go`.

## Related Docs

- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/telemetry/metrics.md`
- `docs/public/reference/telemetry/runtime-signals.md`
- `docs/public/operate/index.md`
- `docs/public/operate/freshness-convergence.md`
- `docs/public/operate/hosted-ops-alert-pack.md`
