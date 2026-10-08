# Metrics Scrape Failing With A Bare 500

## What was reported

After normal read traffic, `GET /metrics` on the API and MCP servers returned a
bare `500 Internal Server Error` (22 bytes) and kept doing so, while `/healthz`
and `/readyz` stayed `200` and the logs were clean. Before the traffic the same
scrape returned about 320 KB.

## Cause

A metric attribute named `service.namespace` sanitizes to the Prometheus label
`service_namespace`. The OTEL Prometheus exporter already adds `service_name` and
`service_namespace` to every series as constant labels
(`WithResourceAsConstantLabels`), so a point-level copy gives the series two
labels with one name. `client_golang` rejects it with
`duplicate label names in constant and variable labels for metric "<name>"`.
`promhttp.HandlerFor` with default options answers a gather error with a 500 for
the whole scrape and writes no log. The runtime composite handler
(`internal/runtime/metrics_handler.go`) then replaces the upstream body with the
status text, which is why the client saw only `Internal Server Error`.

Six call sites passed the attribute: the image-list duration and error
instruments, the three tag-history instruments, and the semantic-search degraded
counter. A series exists only after its first record, so the scrape stayed green
until a request reached one of those handlers.

Disproven alternative: an instrument recorded with different attribute sets in
different calls does not fail a gather in `client_golang` v1.24.1. The shim
recorded one histogram with `{route}`, `{route,outcome}` and `{}` and the scrape
returned 200. Only a duplicate label name, a duplicate series, a type or suffix
collision, or an invalid label fails a series.

## Change

- A meter provider view drops `service.name`, `service.namespace` and their
  underscore forms from every instrument before aggregation.
- The six call sites no longer pass the attribute.
- `/metrics` serves the healthy series when one series fails to gather, logs the
  failing metric as `telemetry.metrics.gather_failed` (one identical line per five
  minutes) and counts `eshu_dp_metrics_gather_errors_total`. A scrape that gathers
  nothing still returns 500.

## Evidence

Baseline (before the change, shim `TestShimServiceNamespaceAttr`, run on
2026-10-08): a counter and a histogram recorded with `service.namespace` returned
status 500 and the body
`2 error(s) occurred: * duplicate label names in constant and variable labels
for metric "eshu_dp_shim_b_total" * ... "eshu_dp_shim_c_seconds"`.

After: `TestScrapeSurvivesAttributesThatShadowResourceLabels` records the four
spellings and gets 200, one series per instrument, the resource values
`service_name="test-service"` and `service_namespace="eshu"`, and the
instrument's own histogram buckets. With the view removed the same test fails,
and the scrape is 200 without the shadowed series while the error log names both
instruments. `TestMetricsHandlerServesHealthySeriesAndReportsGatherError`,
`TestMetricsHandlerRateLimitsRepeatedGatherErrorLogs` and
`TestMetricsHandlerFailsWhenNothingCanBeGathered` cover the handler.

Performance Evidence: scratch benchmark, Apple M-series arm64, Go 1.26, OTEL SDK
metric v1.45.0, exporter prometheus v0.67.0, 3 runs of 2 s.

| Path | Before | After |
| --- | --- | --- |
| `Int64Counter.Add`, 2 attributes | 98.1 to 105.1 ns/op, 16 B, 1 alloc | 136.2 to 137.3 ns/op, 16 B, 1 alloc |
| `GET /metrics`, 400 counters and 400 histograms, 5 series each (4,000 series) | 14.0 to 15.4 ms, 26.06 MB, 218,175 allocs | 13.1 to 14.7 ms, 26.06 MB, 218,174 allocs |

The view adds about 38 ns to each recorded point (the attribute filter). The
scrape path adds one nil check on the no-error path, so its cost is unchanged
within noise.

No-Regression Evidence: record cost rises by about 38 ns per point with no
extra allocation; the scrape cost is unchanged within noise; no queue, lease,
claim, lock, batch, Cypher or worker path is touched. The reporting gatherer
holds one mutex only on the error path, for a string compare and a timestamp.

Observability Evidence: `eshu_dp_metrics_gather_errors_total` rises once per
failing series per scrape. The `telemetry.metrics.gather_failed` error log
carries `error_count` and the exporter error text, which names the failing
instrument. Alert on `increase(eshu_dp_metrics_gather_errors_total[10m]) > 0`.

## Not checked

The fix is proven in the telemetry and query packages only. It is not proven on
a running API or MCP image. Other failure classes that would still drop a series
(for example a type collision between two meters) are reported by the new log and
counter but are not prevented.
