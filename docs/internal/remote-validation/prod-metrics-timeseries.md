# prod-metrics-timeseries — production validation

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
