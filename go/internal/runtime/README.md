# Runtime

## Purpose

`internal/runtime` owns shared process wiring for Eshu binaries: config loading,
admin HTTP muxes, health/readiness, runtime metrics, datastore connection
helpers, retry defaults, memory-limit tuning, API key resolution, recovery
routes, and the opt-in pprof server.

## Ownership boundary

Runtime code provides process-level helpers. It does not own collector,
projector, reducer, query, or storage business logic, and callers should not
fork its datastore, admin-route, retry, or memory-limit contracts.

## Exported surface

See `doc.go` and `go doc ./internal/runtime` for the contract. The stable
anchors are config/lifecycle helpers, admin mux builders, datastore openers,
graph backend parsing, retry policy loading, API key resolution, memory-limit
tuning, pprof wiring, metrics handlers, and status handlers.

## Dependencies

`internal/app` consumes runtime lifecycles from binary wiring.
`internal/buildinfo` supplies runtime identity in metrics. `internal/recovery`
backs recovery admin routes. `internal/status` provides status snapshots for
admin and metrics handlers. `internal/telemetry` provides bootstrap state and
frozen metric/span/log contract names.

## Telemetry

This package emits no OTEL spans of its own. `/metrics` exposes
Prometheus-style runtime gauges and counters with the `eshu_runtime_` prefix,
plus optional OTEL Prometheus output when `WithPrometheusHandler` is set.

## Gotchas / invariants

- `LoadGraphBackend` defaults empty `ESHU_GRAPH_BACKEND` to `nornicdb` and
  rejects unknown values at startup.
- Neo4j and NornicDB both use the shared Bolt driver path in
  `OpenNeo4jDriver`; backend-specific behavior belongs in narrow seams.
- `NewStatusMetricsServer` and `NewPprofServer` can return `(nil, nil)`;
  callers must handle the nil server.
- `NewPprofServer` is gated by `ESHU_PPROF_ADDR`. Port-only values bind to
  `127.0.0.1`.
- `ConfigureMemoryLimit` respects explicit `GOMEMLIMIT` and should run once
  per process after telemetry bootstrap.
- Admin routes are not authenticated by this package. Expose them only through
  operator-controlled network paths.

## Focused tests

```bash
cd go
go test ./internal/runtime -count=1
go vet ./internal/runtime
go run ./cmd/eshu docs verify ../go/internal/runtime --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/run-locally/docker-compose.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/nornicdb-tuning.md`
- `docs/public/reference/graph-backend-installation.md`
