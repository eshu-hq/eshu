# AGENTS.md — internal/runtime guidance for LLM assistants

## Read first

1. `go/internal/runtime/README.md` — pipeline position, exported surface,
   env vars, and operational notes
2. `go/internal/runtime/config.go` — `Config` shape and `LoadConfig`; every
   binary starts here
3. `go/internal/runtime/data_stores.go` — `LoadGraphBackend`, `OpenPostgres`,
   `OpenNeo4jDriver`; the two data-store seams every long-running binary wires
4. `go/internal/runtime/status_server.go` and `status_mux.go` — how
   `NewStatusAdminServer` is assembled from `NewStatusAdminMux` and
   `NewAdminMux`
5. `go/internal/app/app.go` — how binaries compose runtime helpers through
   the Application and Lifecycle contracts
6. `go/cmd/reducer/main.go` — the canonical caller; shows `OpenPostgres`,
   `LoadGraphBackend`, `LoadRetryPolicyConfig`, and
   app.NewHostedWithStatusServer in use together
7. `go/internal/telemetry/instruments.go` and `contract.go` — metric and span
   names before adding new telemetry

## Invariants this package enforces

- **Backend validation at startup** — `LoadGraphBackend` rejects unrecognized
  `ESHU_GRAPH_BACKEND` values with an error; the binary must exit. Never add
  a default or fallback for an unrecognized string. (`data_stores.go:81–92`)

- **Both backends use the same Bolt driver** — `OpenNeo4jDriver` handles both
  `GraphBackendNeo4j` and `GraphBackendNornicDB`; the switch at
  `data_stores.go:290` is the only place that gates Bolt connectivity to
  backend. Do not fork this logic into callers.

- **Admin endpoints are not authenticated** — `NewAdminMux` mounts `/healthz`,
  `/readyz`, `/admin/status`, `/metrics`, and optionally `/admin/replay` and
  `/admin/refinalize` without authentication. These must be served only on the
  admin/metrics port, never on the public API port.

- **Retry defaults are positive** — `LoadRetryPolicyConfig` rejects
  `MaxAttempts ≤ 0` or `RetryDelay ≤ 0`. Do not set either to zero or
  negative, even for tests; use a short positive duration instead.
  (`retry_policy.go:44–49`)

- **`ConfigureMemoryLimit` is a one-shot call** — call it once per process
  after telemetry bootstrap. A second call after `GOMEMLIMIT` is set in the
  env is a no-op but logs redundantly. (`memlimit.go:40–48`)

- **`NewStatusMetricsServer` may return nil** — when `Config.MetricsAddr` is
  empty, the function returns `(nil, nil)`. Every caller of this function must
  check for a nil `*HTTPServer` before calling Start.

## Common changes and how to scope them

- **Add a new env var to `Config`** → add the field to `Config` in `config.go`,
  read it in `LoadConfig`, add validation in `Config.Validate`; update
  `go/internal/runtime/config_test.go` with both a valid and invalid case;
  update `docs/docs/reference/cli-reference.md` or the docker-compose docs
  if the var affects the Compose contract; run
  `go test ./internal/runtime -count=1`.

- **Add a new admin route to `NewStatusAdminServer`** → add a new
  `StatusAdminOption` constructor following the pattern of
  `WithRecoveryHandler` and `WithPrometheusHandler` in `status_server.go`;
  wire it in `NewStatusAdminMux` using the option pattern; update
  `AdminMuxConfig` if the route belongs to the shared probe contract;
  add a test in `status_server_test.go`. Do not change the `NewAdminMux`
  signature unless the route is needed on all admin surfaces.

- **Change Postgres pool defaults** → update the `defaultPostgresXxx` constants
  in `data_stores.go` (lines 16–28); add a `compose_defaults_test.go`
  assertion if the change affects Compose env; update
  `docs/docs/reference/nornicdb-tuning.md` for any NornicDB-relevant pool
  change.

- **Add a new graph backend** → add a `GraphBackend` constant in
  `data_stores.go`; add a case in `LoadGraphBackend`'s switch; add a case in
  `OpenNeo4jDriver` if it uses Bolt; add a test in `data_stores_test.go`;
  update the NornicDB ADR and the embedded-local-backends ADR. Do not add
  `if backend == ...` branches outside documented narrow seams.

- **Expose a new recovery admin route** → add a method to `RecoveryHandler` in
  `recovery_handler.go`; register the route in `RecoveryHandler.Mount`; add
  a test in `recovery_handler_test.go`.

- **Add a new `eshu_runtime_*` metric** → add the emit call in
  renderStatusMetrics in `metrics.go`; verify the gauge name is not already
  defined; add the name to `go/internal/telemetry/contract.go` if it needs a
  span or dimension key; run `go test ./internal/runtime -count=1`.

## Failure modes and how to debug

- Symptom: binary exits with "invalid ESHU_GRAPH_BACKEND" → the env var
  contains an unrecognized string; check `ESHU_GRAPH_BACKEND` in the process
  environment and the Compose service definition.

- Symptom: binary exits with "set ESHU_FACT_STORE_DSN, ESHU_CONTENT_STORE_DSN,
  or ESHU_POSTGRES_DSN" → none of the three Postgres DSN env vars are set;
  check secrets injection in the deployment manifest or `.env` file.

- Symptom: `/readyz` returns 503 → statusReadinessCheck cannot read the
  status snapshot; check Postgres connectivity and `eshu_runtime_queue_*`
  gauges for evidence of store pressure.

- Symptom: `/metrics` endpoint returning only hand-rolled gauges, OTEL data
  missing → `WithPrometheusHandler` was not passed to `NewStatusAdminServer`;
  check that the binary wires `runtimecfg.WithPrometheusHandler(providers.PrometheusHandler)`.

- Symptom: container OOM-killed despite low `eshu_dp_gomemlimit_bytes` →
  `ConfigureMemoryLimit` was not called or `GOMEMLIMIT` env var overrides the
  cgroup-derived limit; check the `source` field in the startup log entry.

## Anti-patterns specific to this package

- **Do not branch on `GraphBackend` outside documented seams** — branches on
  `GraphBackendNornicDB` belong only in `data_stores.go`, the Cypher executor
  seam in `internal/storage/cypher`, and narrow wiring helpers in each `cmd/`.
  Do not add backend branches inside admin handlers, retry policy, or metrics
  rendering.

- **Do not authenticate admin routes here** — authentication for admin
  endpoints is an operator/infrastructure concern (network policy, sidecar
  proxy). Adding auth logic in `NewAdminMux` would couple all binaries to a
  single auth scheme.

- **Do not add global singletons** — `runtime` is imported by many binaries;
  package-level `var` state (not constants) creates cross-binary coupling that
  breaks isolated tests and multi-binary runs in the same process.

- **Do not duplicate pool defaults in callers** — `LoadPostgresConfig` and
  `ConfigurePostgresPool` are the canonical source. Callers that set pool
  values after `OpenPostgres` override the shared contract and produce
  inconsistent behavior across binaries.

## What NOT to change without an ADR

- `LoadGraphBackend` accepted values — adding or removing a valid backend
  string changes the deployment contract for all binaries; see
  `docs/docs/adrs/2026-04-22-nornicdb-graph-backend-candidate.md`.
- Admin route contract (`/healthz`, `/readyz`, `/admin/status`, `/metrics`,
  `/admin/replay`, `/admin/refinalize`) — Kubernetes probes, dashboards, and
  operator runbooks depend on these paths; path or method changes require
  coordinated infra updates.
- `RetryPolicyConfig` defaults — all long-running binaries inherit these;
  changing defaults affects queue drain behavior cluster-wide.
- `Config` field names / env var bindings — CLI and Compose documentation,
  Helm values, and operator runbooks reference these by name.
