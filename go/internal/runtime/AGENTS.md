# Runtime Agent Rules

These rules are mandatory for changes under `go/internal/runtime`.

## Read First

1. `go/internal/runtime/README.md`
2. `go/internal/runtime/config.go`
3. `go/internal/runtime/data_stores.go`
4. `go/internal/runtime/status_server.go`
5. `go/internal/runtime/status_mux.go`
6. `go/internal/app/app.go`
7. `go/cmd/reducer/main.go`
8. `go/internal/telemetry/contract.go`

## Invariants

- `LoadGraphBackend` MUST reject unknown `ESHU_GRAPH_BACKEND` values at
  startup. Do not add silent fallbacks.
- Neo4j and NornicDB MUST share the Bolt driver seam in `OpenNeo4jDriver`.
  Do not fork backend connection logic into callers.
- Admin routes are unauthenticated here. They MUST stay on operator-controlled
  admin or metrics surfaces, not public API ports.
- Retry defaults MUST stay positive. Tests need short positive durations, not
  zero or negative values.
- `ConfigureMemoryLimit` is process setup. Call it once after telemetry
  bootstrap and respect explicit `GOMEMLIMIT`.
- `NewStatusMetricsServer` and `NewPprofServer` may return `(nil, nil)`.
  Callers MUST nil-check before starting servers.
- `NewPprofServer` MUST remain opt-in through `ESHU_PPROF_ADDR`; port-only
  values bind to loopback.

## Change Rules

- New env var: add it to `Config`, `LoadConfig`, validation, runtime tests, and
  the public docs that expose the operator contract.
- New admin route: add an option constructor, wire it through the mux, test it,
  and update public HTTP/CLI docs if the route is operator-facing.
- Postgres pool default change: update the runtime constants, add or update
  Compose/default tests, and update storage or tuning docs when relevant.
- New graph backend: update backend constants, parser, driver seam if needed,
  conformance docs, operations docs, install docs, and tests.
- New pprof exposure in a binary: reuse `ESHU_PPROF_ADDR`; do not add a second
  profiling knob.
- New `eshu_runtime_*` metric: add it in `metrics.go`, verify names and docs,
  and run `go test ./internal/runtime -count=1`.

## Failure Checks

- Invalid backend: inspect `ESHU_GRAPH_BACKEND` in env, Compose, or Helm.
- Missing Postgres DSN: inspect `ESHU_FACT_STORE_DSN`,
  `ESHU_CONTENT_STORE_DSN`, and `ESHU_POSTGRES_DSN`.
- `/readyz` returns 503: inspect Postgres connectivity and runtime queue
  gauges.
- `/metrics` lacks OTEL output: confirm `WithPrometheusHandler` was passed.
- Container OOM with low configured limit: confirm `ConfigureMemoryLimit` ran
  and check the startup log `source`.

## Forbidden Without Architecture-Owner Approval

- Accepted graph backend values.
- Admin route paths or methods.
- Retry policy defaults.
- `Config` field names or environment bindings.
- Backend-specific branches outside documented runtime, command, or Cypher
  seams.
- Global mutable singletons.
