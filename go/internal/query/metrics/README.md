# metrics

## Purpose

Serves `GET /api/v0/metrics/timeseries` for the console's trend panels, and
owns the request-metrics middleware that records per-route latency and
errors for every API and MCP route. See `doc.go` for the contract.

## Ownership boundary

Owns the time-series route, its metric allow-list, the Prometheus/Mimir
`query_range` client, and the request-metrics middleware. Where the source
comes from (`cmd/api/metrics_source.go`) and which muxes the middleware wraps
(`cmd/api`, `cmd/mcp-server`) are decided by the commands.

## Layout

- `handler.go` — `Handler`, `Point`, `RangeQuery`, `TimeSeriesSource`, the
  metric allow-list and freshness mapping.
- `prometheus.go` — `PrometheusTimeSeriesConfig`,
  `PrometheusTimeSeriesSource` and `NewPrometheusTimeSeriesSource`.
- `request.go` — `RequestMiddleware` and its status-capturing writer.
- `capability.go` — `Capability` and `Support`, the single declaration of the
  `platform_metrics.timeseries` row.

## Dependencies

`internal/query/querycontract`, `internal/telemetry`, and the OpenTelemetry
metric API.

## Telemetry

`RequestMiddleware` emits `eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total`, labeled by `route` and `status_class`. The
instruments resolve inside a `sync.Once` on first use, not in a package var,
so a test that installs its own meter provider first still records to it.
Unchanged by the #6642 move.

## Gotchas / invariants

- The route label is the matched mux pattern, never the raw path. Keep it
  that way or label cardinality grows with request traffic.
- The middleware's response writer must keep forwarding `Flush` and `Hijack`:
  the Ask route streams server-sent events through it, and root's
  `ask_sse_metrics_middleware_test.go` fails if streaming breaks.

## Move evidence (#6642)

`metrics.go`, `metrics_prometheus.go`, `request_metrics.go` and their three
tests moved here as `handler.go`, `prometheus.go`, `request.go`,
`handler_test.go`, `prometheus_test.go` and `request_test.go` (`git mv`).
Exported names dropped the `Metrics` prefix the path now carries. The Ask SSE
regression that drives root's `AskHandler` through the middleware stayed in
root as `ask_sse_metrics_middleware_test.go`. The leaf's capability test pins
`Support()`; production registration is proven by root's
`TestCapabilityMatrixMatchesYAMLContract`.

## No-Regression Evidence

No-Regression Evidence: the allow-list, range validation, the Prometheus
client, the freshness mapping and the middleware are unchanged; only package
qualifiers and names differ.

## No-Observability-Change

No-Observability-Change: same metric names, labels and meter; no span or log
changes.

## Failed reads (#7674)

A failed source query on `GET /api/v0/metrics/timeseries` answers the fixed
`metrics query failed`, never the backend error text. The order is
`querycontract.WriteGraphReadError`, then the `errInvalidMetricsRange` 400
(its text is window and step validation only), then
`tracing.WriteServerFailure`: `500` with the error recorded, or `499` when
the caller canceled the request. The Prometheus source cannot produce a
reader fence today; the fence check keeps the shared order.

No-Regression Evidence (#7674): the change runs only after a source query has
already returned an error. No PromQL, query parameter, call count, or success
path changed. A failure now costs one span `RecordError`/`SetStatus` and a
fixed-string write instead of formatting the error into the body.
`go test ./internal/query/... ./internal/queryplan/... -count=1` and
`go test -race ./internal/query/metrics/...` exit 0.

Observability Evidence (#7674): this route has no handler span, so a server
fault marks the request span: the backend error as an `exception` event and
status Error with the fixed message. A client cancel adds
`eshu.request.client_canceled`, leaves the status Unset, and answers `499`.
`server_failure_test.go` asserts both span shapes with a recording provider.

## Related docs

- [Remote validation](../../../../docs/internal/remote-validation/prod-metrics-timeseries.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
