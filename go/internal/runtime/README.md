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

The common startup shape is:

```text
telemetry bootstrap
  -> runtime.LoadConfig
  -> runtime.ConfigureMemoryLimit
  -> runtime.OpenPostgres / runtime.OpenNeo4jDriver
  -> runtime.LoadRetryPolicyConfig
  -> app.NewHostedWithStatusServer
  -> Lifecycle.Start / Stop
```

`NewStatusAdminServer` delegates to `NewStatusAdminMux`, which combines
`/healthz`, `/readyz`, `/admin/status`, `/metrics`, optional recovery routes,
and optional application routes.

## Exported surface

See `doc.go` and `go doc ./internal/runtime` for the full godoc contract. The
main package contracts are:

- `Config`, `LoadConfig`, `Lifecycle`, `HTTPServer`, and `NewHTTPServer`.
- `GraphBackend`, `LoadGraphBackend`, `OpenPostgres`, `OpenNeo4jDriver`,
  `LoadPostgresConfig`, `LoadNeo4jConfig`, `ConfigurePostgresPool`, and
  `ApplyNeo4jConfig`.
- `AdminMuxConfig`, `NewAdminMux`, `NewStatusAdminMux`,
  `NewStatusAdminServer`, `NewStatusMetricsServer`,
  `NewStatusMetricsHandler`, and `NewCompositeMetricsHandler`.
- `RecoveryHandler`, `NewRecoveryHandler`, and `StatusAdminOption` helpers.
- `RetryPolicyConfig` and `LoadRetryPolicyConfig`.
- `ConfigureMemoryLimit`, `ResolveAPIKey`, `NewPprofServer`, and
  `NewObservability`.
- `StatusRequestHandler`, `StatusRequestStore`, `ScanRequest`,
  `ReindexRequest`, and `RequestState`.

## Dependencies

- `internal/app` consumes runtime lifecycles from binary wiring.
- `internal/buildinfo` supplies runtime identity in metrics.
- `internal/recovery` backs recovery admin routes.
- `internal/status` provides status snapshots for admin and metrics handlers.
- `internal/telemetry` provides bootstrap state and frozen metric/span/log
  contract names.

## Telemetry

This package emits no OTEL spans of its own. `/metrics` exposes
Prometheus-style runtime gauges and counters with the `eshu_runtime_` prefix,
including runtime identity, scope refresh status, retry-policy values,
health-state gauges, queue gauges, stage item counts, domain backlog gauges, and
workflow-coordinator status gauges. When `WithPrometheusHandler` is set,
`NewCompositeMetricsHandler` appends OTEL Prometheus output to the same
endpoint.

## Gotchas / invariants

- `LoadGraphBackend` defaults empty `ESHU_GRAPH_BACKEND` to `nornicdb` and
  rejects unknown values at startup.
- Neo4j and NornicDB both use the shared Bolt driver path in
  `OpenNeo4jDriver`; backend-specific behavior belongs only in narrow seams.
- `NewStatusMetricsServer` and `NewPprofServer` can return `(nil, nil)`;
  callers must handle the nil server.
- `NewPprofServer` is gated by `ESHU_PPROF_ADDR`. Port-only values bind to
  `127.0.0.1`.
- `ConfigureMemoryLimit` respects an explicit `GOMEMLIMIT`; call it once per
  process after telemetry bootstrap.
- Admin routes are not authenticated by this package. Expose them only through
  operator-controlled network paths.
- Recovery admin routes mount `/admin/replay` and `/admin/refinalize`; they are
  distinct from command-specific route aliases.

## Verification

Use the smallest command that proves the changed contract:

```bash
cd go
go test ./internal/runtime -count=1
go vet ./internal/runtime
go doc ./internal/runtime
go run ./cmd/eshu docs verify ../go/internal/runtime --limit 1000 \
  --fail-on contradicted,missing_evidence
```

Compose, Helm, admin-route, or runtime-default changes usually need the broader
runtime package tests named by the changed contract.

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/run-locally/docker-compose.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/nornicdb-tuning.md`
- `docs/public/reference/graph-backend-installation.md`
