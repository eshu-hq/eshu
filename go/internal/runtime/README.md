# Runtime

## Purpose

`runtime` owns the shared process wiring used by every Eshu binary at startup.
It provides: admin HTTP muxes, health and readiness probes, status metrics
endpoints, data-store configuration and connection helpers, retry policy
defaults, memory limit tuning, API key resolution, and recovery admin routes.
No binary implements this wiring on its own; each calls the helpers here.

## Where this fits in the pipeline

```mermaid
flowchart TB
  subgraph Binaries
    A["cmd/api"]
    B["cmd/ingester"]
    C["cmd/reducer"]
    D["cmd/projector"]
    E["cmd/workflow-coordinator"]
  end

  subgraph runtime["internal/runtime"]
    LC["LoadConfig\nNewLifecycle"]
    DS["OpenPostgres\nOpenNeo4jDriver\nLoadGraphBackend"]
    AS["NewStatusAdminServer\nNewStatusAdminMux\nNewAdminMux"]
    RP["LoadRetryPolicyConfig"]
    AK["ResolveAPIKey"]
    ML["ConfigureMemoryLimit"]
  end

  A & B & C & D & E --> LC
  B & C & D --> DS
  B & C & D & E --> AS
  B & C & D --> RP
  A --> AK
  B & C --> ML
```

Every binary that hosts long-running work also passes through `internal/app`
which calls `NewLifecycle` and optionally `NewStatusAdminServer` on behalf of
the binary's main function.

## Internal flow

The call sequence for a typical long-running binary (`cmd/ingester`,
`cmd/reducer`, `cmd/projector`):

```mermaid
flowchart TB
  A["main: telemetry.NewBootstrap"] --> B["LoadConfig(serviceName)"]
  B --> C["ConfigureMemoryLimit(logger)"]
  C --> D["OpenPostgres(ctx, os.Getenv)"]
  D --> E["OpenNeo4jDriver(ctx, os.Getenv)\nor LoadGraphBackend + nornicdb path"]
  E --> F["LoadRetryPolicyConfig(os.Getenv, stage)"]
  F --> G["app.NewHostedWithStatusServer\n  -> NewLifecycle(cfg)\n  -> NewStatusAdminServer(cfg, reader, opts...)"]
  G --> H["Application.Run(ctx)\n  -> Lifecycle.Start\n  -> Runner.Run blocks\n  -> Lifecycle.Stop on exit"]
```

`NewStatusAdminServer` delegates to `NewStatusAdminMux`, which calls
`NewAdminMux` to mount `/healthz`, `/readyz`, `/admin/status`, and `/metrics`.
When `WithRecoveryHandler` is passed, `RecoveryHandler.Mount` adds
`/admin/replay`, `/admin/refinalize`, and
`/admin/replay-collector-generations` to the same mux.

## Lifecycle / workflow

`Lifecycle` (from `lifecycle.go:20`) holds `ServiceName` and a
`telemetry.Bootstrap`. Its `Start` method validates the bootstrap contract;
its `Run` method blocks until the context is canceled via `ContextRunner`.
`HTTPServer` (from `http_server.go:23`) also satisfies the Lifecycle
interface defined in `internal/app` — `Start` opens the TCP listener and
serves in the background; `Stop` gracefully drains with a configurable
`ShutdownTimeout` (default 5 s).

ComposeLifecycles in `internal/app` chains multiple Lifecycle values
(including `HTTPServer` instances) into one ordered start/stop chain.

## Exported surface

### Config and env helpers

- `Config` — `ServiceName`, `Command`, `ListenAddr`, `MetricsAddr`; built by
  `LoadConfig(serviceName)` which reads `ESHU_LISTEN_ADDR` (default
  `0.0.0.0:8080`) and `ESHU_METRICS_ADDR` (default `0.0.0.0:9464`)
- `LoadConfig(serviceName)` — validates and returns a `Config`; fails if any
  field is blank

### Data-store helpers

- `GraphBackend` — string type; constants `GraphBackendNeo4j` (`"neo4j"`) and
  `GraphBackendNornicDB` (`"nornicdb"`); `LoadGraphBackend` reads
  `ESHU_GRAPH_BACKEND`, empty defaults to `nornicdb`, invalid values fail at
  startup
- `PostgresConfig` / `PostgresPoolSetter` — config struct and interface for
  pool tuning; loaded by `LoadPostgresConfig` from `ESHU_FACT_STORE_DSN`,
  `ESHU_CONTENT_STORE_DSN`, or `ESHU_POSTGRES_DSN` plus optional pool knobs
- `Neo4jConfig` — driver and pool tuning; loaded by `LoadNeo4jConfig` from
  `ESHU_NEO4J_URI` / `NEO4J_URI`, `ESHU_NEO4J_USERNAME` / `NEO4J_USERNAME`,
  `ESHU_NEO4J_PASSWORD` / `NEO4J_PASSWORD`, and optional pool knobs
- `OpenPostgres(ctx, getenv)` — opens, tunes via `ConfigurePostgresPool`, and
  pings a Postgres connection; returns `*sql.DB`
- `OpenNeo4jDriver(ctx, getenv)` — opens a Neo4j/NornicDB Bolt driver,
  applies `ApplyNeo4jConfig`, verifies connectivity; returns
  `neo4jdriver.DriverWithContext`
- `ConfigurePostgresPool(target, cfg)` — applies `PostgresConfig` to any
  `PostgresPoolSetter`
- `ApplyNeo4jConfig(target, cfg)` — applies `Neo4jConfig` to a
  `*neo4jconfig.Config`

### Admin and HTTP surfaces

- `AdminMuxConfig` / `NewAdminMux` — builds `/healthz`, `/readyz`,
  `/admin/status`, `/metrics` routes; optionally mounts a
  `RecoveryHandler`; service name required
- `HTTPServer` / `HTTPServerConfig` / `NewHTTPServer` — one HTTP server with
  Start/Stop lifecycle; `Addr()` returns the bound address after Start
- `NewStatusAdminServer(cfg, reader, opts...)` — admin `HTTPServer` backed by
  the status reader; used by all long-running binaries
- `NewStatusMetricsServer(cfg, reader, opts...)` — optional dedicated metrics
  `HTTPServer` when `MetricsAddr` differs from `ListenAddr`; returns `nil`
  when `MetricsAddr` is empty
- `NewPprofServer(getenv)` — opt-in `net/http/pprof` `HTTPServer` gated by
  `PprofAddrEnvVar` (`ESHU_PPROF_ADDR`); returns `(nil, nil)` when unset;
  port-only inputs (`:6060`) are rewritten to `127.0.0.1:6060` so the
  default cannot reach beyond the local host
- `NewStatusAdminMux` — lower-level mux builder; combines status handler,
  metrics handler, optional recovery routes, and optional app handler
- `NewStatusMetricsHandler(serviceName, reader)` — Prometheus-style text handler
- `NewCompositeMetricsHandler(statusHandler, prometheusHandler)` — merges
  hand-rolled runtime gauges and OTEL Prometheus output at `/metrics`
- `StatusAdminOption` — option type; constructors: `WithRecoveryHandler`,
  `WithPrometheusHandler`, `WithReadinessProbes`
- `ReadinessProbe` / `ReadinessProbesForDependencies(db, driver)` /
  `PostgresReadinessProbe(db, timeout)` / `GraphReadinessProbe(driver, timeout)`
  — dependency-aware `/readyz` checks. `ReadinessProbesForDependencies` returns
  only the probes for wired (non-nil) dependencies; each probe runs concurrently
  under a bounded timeout and contributes its cause to the `/readyz` failure
  body. The status-snapshot probe (Postgres + schema) always runs as the
  baseline, so default callers keep their existing readiness contract

### Recovery admin

- `RecoveryHandler` / `NewRecoveryHandler(handler)` — mounts `/admin/replay`
  (POST), `/admin/refinalize` (POST), and
  `/admin/replay-collector-generations` (POST) on the admin mux; delegates to
  `recovery.Handler`; replaces the Python write-plane admin surface

### Lifecycle and observability

- `Lifecycle` / `NewLifecycle(cfg)` — validates `Config`, initializes
  `telemetry.Bootstrap`, provides Start / Run / Stop
- `ContextRunner` — zero-value struct; blocks until context is canceled;
  used when a binary has no long-running body of its own
- `Observability` / `NewObservability()` — snapshots `telemetry.MetricDimensionKeys`,
  `telemetry.SpanNames`, `telemetry.LogKeys` at construction time

### Retry policy

- `RetryPolicyConfig` — `MaxAttempts` and `RetryDelay`
- `LoadRetryPolicyConfig(getenv, stagePrefix)` — reads
  `ESHU_{STAGE}_MAX_ATTEMPTS` (default `3`) and `ESHU_{STAGE}_RETRY_DELAY`
  (default `30s`); both must be positive; stage prefix is required

### Memory limits

- `ConfigureMemoryLimit(logger)` — sets `GOMEMLIMIT` from cgroup memory ×
  `DefaultMemLimitRatio` (0.70), floor `MinMemLimit` (512 MiB);
  unconditionally sets `GODEBUG=madvdontneed=1`; respects explicit
  `GOMEMLIMIT` env var as highest priority

### API key

- `ResolveAPIKey(getenv)` — resolution order: explicit `ESHU_API_KEY` env,
  then persisted `ESHU_HOME/.env`, then auto-generated 32-byte hex token when
  `ESHU_AUTO_GENERATE_API_KEY` is truthy; writes generated tokens back to the
  env file under `.env.lock` so follow-on CLI and service processes reuse the
  same token

### Status requests

- `StatusRequestStore` — interface for durable scan/reindex lifecycle ops
- `StatusRequestHandler` / `NewStatusRequestHandler(store)` — manages
  `RequestScan`, `ClaimScan`, `CompleteScan`, `RequestReindex`,
  `ClaimReindex`, `CompleteReindex`
- `RequestState` — `idle`, `pending`, `running`, `completed`, `failed`
- `ScanRequest` / `ReindexRequest` — lifecycle state structs

## Dependencies

| Package | Used for |
| --- | --- |
| `internal/buildinfo` | `AppVersion()` in runtime metrics labels |
| `internal/recovery` | `recovery.Handler` backing `RecoveryHandler` |
| `internal/status` | `statuspkg.Reader` for admin and metrics handlers |
| `internal/telemetry` | `Bootstrap`, `MetricDimensionKeys`, `SpanNames`, `LogKeys`, `SkippedRefreshCount`, `DefaultServiceNamespace` |

## Telemetry

This package emits no OTEL spans or traces of its own. The metrics endpoint
at `/metrics` exposes hand-rolled Prometheus-style gauges derived from the
`statuspkg.Reader`. Metric names (all `eshu_runtime_` prefix):

- `eshu_runtime_info` — binary identity labels (service name, namespace, version)
- `eshu_runtime_scope_active`, `eshu_runtime_scope_changed`, `eshu_runtime_scope_unchanged`
- `eshu_runtime_refresh_skipped_total`
- `eshu_runtime_retry_policy_max_attempts`, `eshu_runtime_retry_policy_retry_delay_seconds`
- `eshu_runtime_health_state` — labeled `state` (healthy/progressing/degraded/stalled)
- `eshu_runtime_queue_total`, `eshu_runtime_queue_outstanding`, and queue depth gauges
- `eshu_runtime_stage_items` — labeled by `stage` and `status`
- `eshu_runtime_domain_outstanding` and per-domain backlog gauges
- `eshu_runtime_collector_generation_dead_letter`,
  `eshu_runtime_collector_generation_replay_requested`,
  `eshu_runtime_collector_generation_replay_attempts`, and
  `eshu_runtime_collector_generation_dead_letter_oldest_age_seconds`
- `eshu_runtime_coordinator_*` — coordinator claim and completeness counters

When `WithPrometheusHandler` is set, `NewCompositeMetricsHandler` appends OTEL
Prometheus output after the hand-rolled gauges at the same `/metrics` endpoint.

## Operational notes

- `/healthz` (liveness) returns `200 OK` unconditionally when no `AdminCheck`
  is wired; it is intentionally dependency-free so a transient Postgres or graph
  outage never restarts an otherwise healthy process.
- `/readyz` (readiness) runs the status-snapshot probe (Postgres + schema)
  plus any probes registered via `WithReadinessProbes`. The API and MCP server
  register `PostgresReadinessProbe` (bounded `PingContext`) and
  `GraphReadinessProbe` (bounded Bolt `VerifyConnectivity`, covering both Neo4j
  and NornicDB). Each probe runs concurrently under its own bounded timeout; a
  failure returns `503` with a cause body naming every failing dependency
  (e.g. `graph: ...; postgres: ...`). A nil graph driver (local lightweight
  profile) reports ready so readiness is not gated on an unused dependency.
- Readiness anti-flap is handled at the Kubernetes probe layer
  (`readinessProbe.failureThreshold`), not by in-process state, so the endpoint
  always reports true current dependency state. See
  [Health And Readiness Probes](../../../docs/public/reference/health-readiness.md).

  No-Regression Evidence: dependency probes run only on `/readyz` hits at the
  Kubernetes probe cadence (default `periodSeconds: 15`), never on the query or
  graph-write hot paths. Each probe is a single bounded connection check
  (`PingContext` / `VerifyConnectivity`, default 2s timeout) executed
  concurrently; `TestRunReadinessProbeBoundsSlowDependency` confirms a blocked
  dependency returns in well under one second. Backend: NornicDB (Bolt) and
  Postgres via the existing shared pools; no new pool, worker, or queue is
  introduced. Verified by `go test ./internal/runtime ./cmd/api ./cmd/mcp-server
  -count=1`.

  Observability Evidence: `/readyz` now distinguishes alive-but-broken from
  ready — the `503` body names the failing dependency and its error, so an
  operator can tell graph-down from Postgres pool-exhaustion from
  schema-not-applied (status_snapshot) without shelling into the pod. Liveness
  (`/healthz`) stays dependency-free. The Helm `readinessProbe.failureThreshold`
  (3) debounces transient blips before pulling the pod from Service endpoints.
- `eshu_runtime_queue_oldest_outstanding_age_seconds` aging means workers
  cannot keep up with ingest rate; investigate worker count and graph backend
  latency before changing pool sizes.
- `eshu_runtime_health_state{state="stalled"}` = 1 means the pipeline is not
  making progress; check structured logs and failure_class before restarting.
- `eshu_runtime_collector_generation_dead_letter` > 0 means a collector commit
  failed before normal projector work items existed. Fix the commit failure,
  then use `/admin/replay-collector-generations` with a collector kind and
  bounded limit to request source-level replay.
- Admin endpoints have no authentication. They must be bound to the admin port
  (default `0.0.0.0:9464`) and not exposed on the public API port.
- `compose_defaults_test.go` enforces that `docker-compose.yaml` sets
  `ESHU_GRAPH_BACKEND=nornicdb` for all graph runtime services and that the
  telemetry overlay is never mixed into a run without an explicit base file.
- `compose_nornicdb_image_test.go` enforces that the default NornicDB Compose
  image is a pinned multi-arch manifest and that Compose does not force an
  amd64 platform when the operator leaves the Compose platform override unset.

## Extension points

- `StatusAdminOption` — add new admin mux behavior by defining a new
  `WithPrometheusHandler`-style constructor returning a `StatusAdminOption`;
  do not mutate `AdminMuxConfig` directly
- `PostgresPoolSetter` — any `*sql.DB`-like type satisfies the interface;
  use `ConfigurePostgresPool` to apply shared defaults without forking the
  tuning logic
- `AdminMuxConfig.Health` and `AdminMuxConfig.Ready` — supply custom
  `AdminCheck` functions to gate the probes on domain-specific invariants

## Gotchas / invariants

- `LoadGraphBackend` with an unrecognized value fails at startup, not at
  first use. `data_stores.go:90` is the only valid switch for the backend
  env var; do not add new backend strings without updating this switch and
  the NornicDB ADR.
- `OpenNeo4jDriver` returns an error when `ESHU_GRAPH_BACKEND` is not
  `neo4j` or `nornicdb` (`data_stores.go:290`). Both backends use the same
  Bolt driver path.
- `NewStatusMetricsServer` returns `(nil, nil)` when `MetricsAddr` is empty.
  Callers must handle the nil return; MountStatusServer in `internal/app`
  checks this.
- `NewPprofServer` returns `(nil, nil)` when `ESHU_PPROF_ADDR` is unset or
  whitespace-only, matching the `NewStatusMetricsServer` precedent. Callers
  must check the nil return before calling Start. Port-only inputs are
  rewritten to `127.0.0.1` to keep the default exposure on loopback;
  explicit hosts (`0.0.0.0`, named hosts) are preserved.
- `ConfigureMemoryLimit` is a no-op when `GOMEMLIMIT` is already set as an
  env var; it logs the existing value and returns 0. Do not call it twice.
- Admin routes are not authenticated by this package. If the admin port is
  exposed outside a pod, the operator is responsible for network controls.

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/run-locally/docker-compose.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/local-testing.md`
- ADR: `docs/public/reference/backend-conformance.md`
- ADR: `docs/public/reference/graph-backend-operations.md`
