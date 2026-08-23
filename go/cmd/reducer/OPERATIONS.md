# Reducer operations and telemetry

This companion to the [`cmd/reducer` overview](README.md) collects the
operator-facing signals, scaling guidance, and runtime invariants for the
`resolution-engine` service.

## Telemetry

- Logger scope: `reducer`, component `reducer`.
- Tracer: `providers.TracerProvider.Tracer(telemetry.DefaultSignalName)`.
- Postgres instrumentation: `postgres.InstrumentedDB{StoreName: "reducer"}`.
- Graph instrumentation: `sourcecypher.InstrumentedExecutor`.
- Queue depth: `postgres.NewQueueObserverStore` → `telemetry.RegisterObservableGauges`.
- Cross-scope completion: the same queue gauges report
  `queue=cross_scope_completion.<producer_domain>`; successful cycles log the
  producer domain, coalesced producer item count, scheduled canonical consumer
  count, and fanout duration, while failures log the fenced retry error.
- Generation retention: bounded cleanup cycles emit generation, row, skip-reason,
  duration, batch-size, oldest-eligible-age, and failure metrics without raw
  scope or generation identifiers.
- Graph orphan sweep: `eshu_dp_graph_orphan_nodes` reports bounded
  zero-relationship node counts by closed `node_label`; cycle logs include
  lease acquisition, counts, marks, deletes, duration, and failure class.
- Search vector build: opt-in local vector build cycles log scanned scopes,
  attempted scopes, document counts, vector counts, failed documents, duration,
  and `failure_class=search_vector_build_error` on build or pending scan
  failures.
- Admission decisions: mapped reducer domains persist bounded explainability
  rows in `admission_decisions` after their existing canonical writers succeed;
  the path adds no raw provider locators or graph writes.
- Admin surface: `/healthz`, `/readyz`, `/metrics`, `/admin/status` via
  `app.NewHostedWithStatusServer`.

## Operational notes

- The graph backend is selected via `ESHU_GRAPH_BACKEND` (default `nornicdb`),
  and invalid values fail at startup. The Postgres DSN is configured via
  `ESHU_POSTGRES_DSN`.
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
- Local semantic/hybrid search needs the reducer build flag and API/MCP read
  flag enabled independently. Enabling the reducer flag alone builds ready
  sidecar rows but does not change public route behavior.
- In that same local-authoritative NornicDB profile, `CodeCallProjectionRunner`
  is wired with `NewReducerGraphDrain` so code-call edge projection waits until
  reducer-owned graph domains have drained. Keep this as a scheduling gate, not
  a graph-truth shortcut.

## Gotchas and invariants

- Invalid ESHU_GRAPH_BACKEND values fail at startup via
  `runtimecfg.LoadGraphBackend`.
- ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true is only for conformance
  validation; grouped canonical writes on NornicDB are not promoted to
  production default.
- Handler code must not branch on graph backend type directly; backend
  differences belong in `storage/cypher` narrow seams only.
- Handlers depend on `graph_projection_phase_state` rows published by the
  projector; missing phase publications cause edge domains to block.
