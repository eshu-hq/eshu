# prod-metrics-timeseries — production validation

Validation-Slug: prod-metrics-timeseries
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_metrics.timeseries passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: platform_metrics.timeseries -> http:GET /api/v0/metrics/timeseries?metric=queue_depth&window=1h&step=5m

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `platform_metrics.timeseries` (tool `get_metrics_time_series`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: one_metric_window_2000_samples`, `p95_latency_ms: 2500`,
`max_truth_level: derived`.

## Claim validated

Bounded HTTP metric-history read backed by a configured Prometheus/Mimir
collector target; returns an explicit empty/unavailable state rather than a
guess when no source is configured.

## Committed reproducible evidence

**Handler behavior, range validation, capability registration** —
`go/internal/query/metrics_test.go`:
`TestTimeSeriesRejectsUnknownMetric`,
`TestTimeSeriesEmptyPointsWhenNoSourceConfigured`,
`TestTimeSeriesReturnsSourcePoints`,
`TestTimeSeriesEmptyHistoryIsBuildingNotError`,
`TestTimeSeriesRejectsInvalidRangeAsBadRequest`,
`TestTimeSeriesCapabilityIsRegistered`. Reproduce:

```bash
cd go && go test ./internal/query -run TestTimeSeries -count=1
```

**Prometheus/Mimir range-API source and bound enforcement** —
`go/internal/query/metrics_prometheus_test.go`:
`TestPrometheusMetricsTimeSeriesSourceQueriesRangeAPI`,
`TestPrometheusMetricsTimeSeriesSourceRejectsUnboundedRanges`,
`TestPrometheusMetricExpressionsCoverSupportedMetrics`. Reproduce:

```bash
cd go && go test ./internal/query -run TestPrometheusMetrics -count=1
```

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
