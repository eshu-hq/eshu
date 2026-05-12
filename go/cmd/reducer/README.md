# cmd/reducer

`cmd/reducer` builds the `eshu-reducer` binary — the long-running
`resolution-engine` runtime that drains the reducer fact-work queue,
executes domain handlers, materializes cross-domain truth, and writes
shared edges into the configured graph backend. The deployed service
identity is `resolution-engine`.

## Where this fits in the pipeline

```mermaid
flowchart LR
  Ingester["Ingester\n(fact emission)"] --> ProjQ["Projector\nqueue"]
  ProjQ --> Projector["internal/projector\n(source-local projection)"]
  Projector --> ReducerQ["Reducer queue\n(Postgres)"]
  ReducerQ --> Reducer["eshu-reducer\n(this binary)"]
  Reducer --> Graph["Graph backend\n(Neo4j / NornicDB)"]
  Reducer --> PhaseState["graph_projection_phase_state\n(Postgres)"]
  PhaseState --> SharedRunners["SharedProjectionRunner\nCodeCallProjectionRunner\nRepoDependencyProjectionRunner"]
  SharedRunners --> Graph
```

## Internal flow

```mermaid
flowchart TB
  run["run()"] --> Telemetry["telemetry.NewBootstrap\nNewProviders"]
  run --> DB["runtimecfg.OpenPostgres"]
  run --> Graph["openReducerNeo4jAdapters"]
  run --> Build["buildReducerService()"]
  Build --> Handlers["DefaultHandlers wired\n(all domain handlers)"]
  Build --> Queue["postgres.NewReducerQueue"]
  Build --> Runners["SharedProjectionRunner\nCodeCallProjectionRunner\nRepoDependencyProjectionRunner\nGraphProjectionPhaseRepairer"]
  Build --> SvcObj["reducer.Service"]
  SvcObj --> AdminSurface["app.NewHostedWithStatusServer\n/healthz /readyz /metrics /admin/status"]
  AdminSurface --> RunLoop["service.Run(ctx)\nblocks until SIGINT/SIGTERM"]
```

## Startup sequence

1. `telemetry.NewBootstrap("reducer")` + `telemetry.NewProviders` — OTEL
   logger, tracer, meter, Prometheus handler.
2. `runtimecfg.OpenPostgres` — Postgres connection from ESHU_POSTGRES_DSN.
3. `postgres.NewQueueObserverStore` + `telemetry.RegisterObservableGauges`
   — queue-depth observable gauges.
4. `openReducerNeo4jAdapters` — opens graph-backend driver; backend is
   chosen by ESHU_GRAPH_BACKEND (default `nornicdb`). Invalid values fail
   at startup.
5. `buildReducerService` — loads all config, wires `DefaultHandlers`
   (including the Terraform config-vs-state drift adapters
   `TerraformBackendResolver`, `DriftEvidenceLoader`, and `DriftLogger`
   activated for chunk #163; the evidence loader carries the runtime
   `Tracer`, `Logger`, and `Instruments` so its four-query join opens
   `SpanReducerDriftEvidenceLoad`, surfaces decode-failure WARN logs,
   and increments
   `eshu_dp_drift_unresolved_module_calls_total` per unresolvable
   `module {}` source per the issue #169 module-aware join),
   `SharedProjectionRunner`, `CodeCallProjectionRunner`,
   `RepoDependencyProjectionRunner`, `GraphProjectionPhaseRepairer`, and
   the `postgres.NewReducerQueue`.
6. `app.NewHostedWithStatusServer` — mounts the shared admin surface.
7. `signal.NotifyContext` for `os.Interrupt` / `syscall.SIGTERM`.
8. `service.Run(ctx)` — blocks until the context is canceled; hosted
   runtime drains in-flight work before returning.

## Configuration reference

All env vars parsed in `config.go` and `neo4j_wiring.go`.

### Queue and retry

| Variable | Default | Purpose |
| --- | --- | --- |
| `ESHU_REDUCER_RETRY_DELAY` | `30s` | Delay before a failed intent becomes re-claimable |
| `ESHU_REDUCER_MAX_ATTEMPTS` | `5` | Terminal failure threshold |
| `ESHU_REDUCER_WORKERS` | `NumCPU` NornicDB / `min(NumCPU,4)` Neo4j | Concurrent intent workers |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | `workers` NornicDB / `workers×4 (max 64)` Neo4j | Items per claim batch |
| `ESHU_REDUCER_CLAIM_DOMAIN` | `""` (all domains) | Restrict claims to one `Domain` |

### Claim gating

| Variable | Purpose |
| --- | --- |
| `ESHU_QUERY_PROFILE` | With ESHU_GRAPH_BACKEND=nornicdb, `local-authoritative` enables the projector drain gate |
| `ESHU_REDUCER_EXPECTED_SOURCE_LOCAL_PROJECTORS` | Semantic-entity claims wait until this many source-local projectors have published |
| `ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT` | Cap on concurrent semantic-entity claims (default `1` on NornicDB) |

### Shared projection

Parsed by `LoadSharedProjectionConfig` in `internal/reducer`.

| Variable | Default | Purpose |
| --- | --- | --- |
| ESHU_SHARED_PROJECTION_PARTITION_COUNT | `8` | Partitions per domain |
| ESHU_SHARED_PROJECTION_BATCH_LIMIT | `100` | Intents per batch |
| ESHU_SHARED_PROJECTION_POLL_INTERVAL | `500ms` | Base poll interval |
| ESHU_SHARED_PROJECTION_LEASE_TTL | `60s` | Partition lease TTL |
| ESHU_SHARED_PROJECTION_WORKERS | `min(NumCPU,4)` | Concurrent partition workers |

### Code-call projection

| Variable | Default |
| --- | --- |
| `ESHU_CODE_CALL_PROJECTION_POLL_INTERVAL` | `500ms` |
| `ESHU_CODE_CALL_PROJECTION_LEASE_TTL` | `60s` |
| `ESHU_CODE_CALL_PROJECTION_BATCH_LIMIT` | `100` |
| `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` | `250000` |
| `ESHU_CODE_CALL_PROJECTION_LEASE_OWNER` | `code-call-projection-runner` |

### Repo-dependency projection

| Variable | Default |
| --- | --- |
| `ESHU_REPO_DEPENDENCY_PROJECTION_POLL_INTERVAL` | `500ms` |
| `ESHU_REPO_DEPENDENCY_PROJECTION_LEASE_TTL` | `60s` |
| `ESHU_REPO_DEPENDENCY_PROJECTION_BATCH_LIMIT` | `100` |

### Edge writers

| Variable | Default | Purpose |
| --- | --- | --- |
| `ESHU_CODE_CALL_EDGE_BATCH_SIZE` | `1000` | Code-call edge rows per graph write |
| `ESHU_CODE_CALL_EDGE_GROUP_BATCH_SIZE` | `1` | Code-call grouped-write batch |
| `ESHU_INHERITANCE_EDGE_GROUP_BATCH_SIZE` | `1` | Inheritance grouped-write batch |
| `ESHU_SQL_RELATIONSHIP_EDGE_GROUP_BATCH_SIZE` | `1` | SQL relationship grouped-write batch |

### Repair runner

| Variable | Default |
| --- | --- |
| `ESHU_GRAPH_PROJECTION_REPAIR_POLL_INTERVAL` | `1s` |
| `ESHU_GRAPH_PROJECTION_REPAIR_BATCH_LIMIT` | `100` |
| `ESHU_GRAPH_PROJECTION_REPAIR_RETRY_DELAY` | `1m` |

### Terraform drift

| Variable | Default | Purpose |
| --- | --- | --- |
| `ESHU_DRIFT_PRIOR_CONFIG_DEPTH` | `10` | Number of prior repo-snapshot generations the drift loader walks to detect `removed_from_config`. `0` means use the default (10). Invalid or non-positive input falls back to `0` with a structured WARN log and the env key `failure_class=env_parse`. Parsed by `parsePriorConfigDepth` in `config.go:270` and stored in `PostgresDriftEvidenceLoader.PriorConfigDepth`. |

### NornicDB knobs (narrow seam)

| Variable | Purpose |
| --- | --- |
| `ESHU_CANONICAL_WRITE_TIMEOUT` | Per-write timeout for NornicDB canonical writes (default `30s`) |
| `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` | Enable NornicDB semantic grouped writes for conformance testing |
| `ESHU_NORNICDB_SEMANTIC_ENTITY_LABEL_BATCH_SIZES` | Override label batch sizes for NornicDB semantic entity writes |

### Profiling

| Variable | Purpose |
| --- | --- |
| `ESHU_PPROF_ADDR` | Opt-in `net/http/pprof` endpoint via `runtime.NewPprofServer`; unset disables; port-only inputs (`:6060`) bind to `127.0.0.1` |

## Exported surface

`buildReducerService` (unexported) returns a `reducer.Service` value. The
binary itself exports nothing; all domain logic is owned by
`internal/reducer`. Wiring-level adapters in `neo4j_wiring.go` expose
unexported executor adapters (`reducerNeo4jExecutor`,
`reducerCypherExecutor`) used only inside this package.

The direct process contract includes `eshu-reducer --version` and
`eshu-reducer -v`. Both flags print the build-time version through
`buildinfo.PrintVersionFlag` before telemetry, Postgres, or graph setup begins.

## Dependencies

- `internal/reducer` — `Service`, `DefaultHandlers`, all domain handler
  types, `SharedProjectionRunner`, `CodeCallProjectionRunner`,
  `RepoDependencyProjectionRunner`, `GraphProjectionPhaseRepairer`
- `internal/storage/postgres` — `NewReducerQueue`, `InstrumentedDB`,
  `NewSharedIntentStore`, `NewGraphProjectionPhaseStateStore`,
  `NewGraphProjectionPhaseRepairQueueStore`, `NewReducerGraphDrain`, all
  fact/relationship stores
- `internal/storage/cypher` — `InstrumentedExecutor`, `NewEdgeWriter`
- `internal/runtime` — `OpenPostgres`, `LoadGraphBackend`, retry policy
- `internal/query` — `GraphQuery` port, `ParseQueryProfile`
- `internal/app` — `NewHostedWithStatusServer`
- `internal/telemetry` — bootstrap, providers, instruments

Graph writes flow through `storage/cypher.EdgeWriter` and
`storage/cypher.InstrumentedExecutor`, never through a raw driver.

The graph backend is selected via ESHU_GRAPH_BACKEND (default `nornicdb`).
Invalid values fail at startup. The Postgres DSN is configured via
ESHU_POSTGRES_DSN.

## Telemetry

- Logger scope: `reducer`, component `reducer`.
- Tracer: `providers.TracerProvider.Tracer(telemetry.DefaultSignalName)`.
- Postgres instrumentation: `postgres.InstrumentedDB{StoreName: "reducer"}`.
- Graph instrumentation: `sourcecypher.InstrumentedExecutor`.
- Queue depth: `postgres.NewQueueObserverStore` → `telemetry.RegisterObservableGauges`.
- Admin surface: `/healthz`, `/readyz`, `/metrics`, `/admin/status` via
  `app.NewHostedWithStatusServer`.

## Operational notes

- Scale the `resolution-engine` Deployment when queue age rises and workers
  remain busy. Do not scale it to fix Postgres saturation — fix database
  pressure first.
- Version probes are pre-startup checks. Keep `buildinfo.PrintVersionFlag` at
  the top of `main` so operators can inspect the reducer binary without opening
  queues or graph drivers.
- In Kubernetes, size the Postgres connection pool to accommodate
  `ESHU_REDUCER_WORKERS × replica_count` concurrent connections.
- On NornicDB, the default reducer worker count now matches host CPU count.
  Lower it only when queue, conflict-key, and graph-write telemetry show
  backend saturation or unsafe overlap.
- Worker leases renew at `LeaseDuration / 2`; a retry delay shorter than
  the lease TTL causes claims to churn.
- The projector drain gate (ESHU_QUERY_PROFILE=local-authoritative +
  ESHU_GRAPH_BACKEND=nornicdb) delays semantic-entity claims until
  source-local projectors have finished.
- In that same local-authoritative NornicDB profile, `CodeCallProjectionRunner`
  is wired with `NewReducerGraphDrain` so code-call edge projection waits until
  reducer-owned graph domains have drained. Keep this as a scheduling gate, not
  a graph-truth shortcut.

## Gotchas / invariants

- Invalid ESHU_GRAPH_BACKEND values fail at startup via
  `runtimecfg.LoadGraphBackend`.
- ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true is only for conformance
  validation; grouped canonical writes on NornicDB are not promoted to
  production default.
- Handler code must not branch on graph backend type directly; backend
  differences belong in `storage/cypher` narrow seams only.
- Handlers depend on `graph_projection_phase_state` rows published by the
  projector; missing phase publications cause edge domains to block.

## Related docs

- [Service runtimes — Resolution Engine](../../../docs/docs/deployment/service-runtimes.md#resolution-engine)
- [NornicDB tuning](../../../docs/docs/reference/nornicdb-tuning.md)
- [Telemetry overview](../../../docs/docs/reference/telemetry/index.md)
- `go/internal/reducer/README.md`
