# Observability Deployment

## Purpose

`deploy/observability` contains the deployable Prometheus alert rules and local
OpenTelemetry Collector configuration for Eshu. It is configuration, not the
source of truth for metric names or runtime telemetry contracts.

## Files

- `otel-collector-config.yaml` receives OTLP traces and metrics, forwards traces
  to Jaeger, and exposes Prometheus metrics on the collector.
- `alerts.yaml` carries standalone Prometheus alert groups.
- `prometheus-rule.yaml` carries the Kubernetes `PrometheusRule` version of the
  same alert intent for Prometheus Operator deployments.

## Ownership Boundary

This directory owns deployable alert and collector configuration. Go metric
instruments live in `go/internal/telemetry`; runtime health and status semantics
live in `go/internal/status`; operator reference material lives under
`docs/public/reference/telemetry/`.

When an alert expression changes because a metric contract changed, update the
telemetry code and public telemetry docs in the same change.

## Alert Coverage

Current alert groups cover:

- pipeline freshness, projection failures, reducer failures, shared projection
  backlog, and collector stalls
- API and MCP error rate and latency
- Postgres and graph backend latency/errors
- fact emission, projection throughput, and reducer intent backlog
- Terraform-state collector claim backlog, parse latency, conditional reads,
  unknown provider schema growth, and warning emission

Read `alerts.yaml` or `prometheus-rule.yaml` for exact expressions, thresholds,
labels, and annotations. Do not keep a second threshold catalog in this README.

## Deployment

Standalone Prometheus deployments load `alerts.yaml` as a normal rule file.
Kubernetes deployments that use kube-prometheus-stack apply
`prometheus-rule.yaml`.

The OTEL Collector config is intentionally small: OTLP receivers, a batch
processor, Jaeger trace export, Prometheus metric export, and separate
trace/metric pipelines.

## Verification

After changing alerts or collector wiring:

- validate YAML with the deployment tool used by the target environment
- check Prometheus or Prometheus Operator has loaded the `eshu.*` rule groups
- confirm alert expressions reference metric names registered in
  `go/internal/telemetry/instruments.go`
- confirm runbook links and annotations point at current telemetry docs

## Gotchas / Invariants

- Alert expressions must use bounded metric labels. Repository paths, file
  paths, raw locators, package names, image digests, work-item IDs, and cloud
  resource identifiers do not belong in Prometheus labels.
- Keep standalone Prometheus and `PrometheusRule` alert intent aligned.
- Prefer changing thresholds in the rule files, not in prose.
- NornicDB is the default graph backend in current runtime docs, but some alert
  metric names still include legacy `neo4j` naming. Do not rename those metrics
  without a telemetry compatibility plan.

## Related Docs

- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/telemetry/metrics.md`
- `docs/public/reference/telemetry/runtime-signals.md`
- `docs/public/reference/telemetry/logs.md`
- `docs/public/operate/index.md`
