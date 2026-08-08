# mock-prometheus-mimir

## Purpose

`mock-prometheus-mimir` supplies a deterministic Prometheus-compatible
`query_range` endpoint for the golden-corpus gate. It lets the deployed Eshu API
prove a positive metric-history read without credentials or an external metrics
service.

## Ownership boundary

This command owns two test-only endpoints: `GET /health` and strict
`GET /api/v1/query_range`. Eshu's metric-name mapping, source configuration,
range bounds, response decoding, and public API remain in
`go/internal/query` and `go/cmd/api`.

## Exported surface

The package exports no Go API. The binary reads
`MOCK_PROMETHEUS_MIMIR_LISTEN_ADDR`, which defaults to `127.0.0.1:19090`.

## Dependencies

The command uses only the Go standard library and `go/internal/buildinfo` for
the version flag.

## Telemetry

The process writes a startup record through `log/slog`. It does not register
OpenTelemetry providers or Eshu product metrics.

## Gotchas / invariants

- `X-Scope-OrgID` must equal the public fixture tenant `golden-corpus`.
- The only accepted PromQL expression is Eshu's closed queue-depth mapping.
- The requested window must be exactly one hour with a `5m` step.
- The response always moves from queue depth `2` to `0`; timestamps come from
  the requested range so samples remain inside the caller's window.

## Related docs

- `go/internal/query/metrics_prometheus.go` owns the production range client.
- `go/cmd/api/metrics_source.go` selects the configured Prometheus/Mimir target.
- `scripts/verify-golden-corpus-gate.sh` runs the deployed proof.
