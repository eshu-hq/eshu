# AGENTS.md — internal/projector guidance for LLM assistants

## Read first

1. `go/internal/projector/README.md` — pipeline position, lifecycle, exported
   surface, and operational notes
2. `go/internal/projector/service.go` — `Service.Run`, the poll-and-dispatch
   loop; understand `processWork` before touching concurrency
3. `go/internal/projector/runtime.go` — `Runtime.Project`; the four write
   stages and their ordering
4. `go/internal/projector/canonical.go` and `canonical_builder.go` — the
   `CanonicalMaterialization` shape and how it is built from facts
5. `go/internal/telemetry/instruments.go` and `contract.go` — metric and span
   names before adding new telemetry

## Invariants this package enforces

- **Idempotency** — every write path must converge on the same graph truth on
  retries. `doc.go` states this as a package invariant; `runtime_retry_test.go`
  tests it.
- **Phase publish before ack** — `publishCanonicalGraphPhases` (defined in
  `runtime.go:181`) must succeed before the work item acks. The publish call
  itself is at `runtime.go:190`; if it fails and `RepairQueue` is non-nil, a
  repair row is enqueued.
- **Module/Parameter exclusion from generic entity phase** — `Module` and
  `Parameter` labels are skipped in `extractEntities` because they use different
  graph MERGE keys. Enforced at `canonical_builder.go:227-229`.
- **Repo-qualified paths** — `FileRow.Path` and `EntityRow.FilePath` are set to
  `repoPath/relative_path` to avoid cross-repo MERGE collisions. Enforced in
  `extractFiles` and `extractEntities` via `qualifyPath`.
- **Directory sort order** — `buildDirectoryChain` sorts by `Depth` ascending so
  parent directories exist before children during graph writes
  (`canonical_builder.go:191`).
- **ReducerIntent stable ordering** — `intents` are sorted by `Domain`,
  `EntityKey`, then `FactID` before enqueue (`runtime.go:308`). Do not remove
  this sort.
- **CanonicalWriter interface boundary** — no caller in this package calls a Neo4j
  or NornicDB driver directly. All canonical writes go through `CanonicalWriter`.
  Backend-specific logic belongs in `internal/storage/cypher` adapters.

## Common changes and how to scope them

- **Add a new entity type** → add to `entityTypeLabelMap` in `canonical.go`,
  add a schema constraint in the graph schema file, run
  `go test ./internal/projector -count=1`. Why: `EntityTypeLabel` and
  `extractEntities` both gate on this map; missing entries silently drop nodes.

- **Add a new projection stage write** → add to `Runtime.Project` in
  `runtime.go`; add `ProjectorStageDuration` recording with the new stage label
  in `runtime_stages.go`; add a span if the stage crosses a service boundary;
  add a test in `runtime_test.go`. Why: all stage telemetry is labeled and must
  appear in the telemetry contract at `go/internal/telemetry/contract.go`.

- **Change concurrency behavior** → touch `service.go` `runConcurrent` and the
  large-generation semaphore; run `service_test.go` and `service_shutdown_test.go`;
  read `docs/docs/reference/telemetry/index.md` for `eshu_dp_large_repo_semaphore_wait_seconds`
  guidance. Why: worker goroutines share a cancel context; wrong cancellation
  propagation causes silent dropped work.

- **Add a new reducer domain intent** → add the domain constant in
  `internal/reducer`, add intent construction in `buildReducerIntent` or a
  new `build*ReducerIntent` helper in `runtime.go` or `semantic_entity_intents.go`,
  add a test in `stage_relationships_test.go` or the semantic intents test files.
  Why: intent domain values must be parseable by `reducer.ParseDomain`.

## Failure modes and how to debug

- Symptom: `eshu_dp_projections_completed_total{status="failed"}` rising →
  likely cause: graph backend unavailable or fact validation error → check
  structured log `failure_class` field; `dependency_unavailable` is retryable,
  `projection_bug` needs code investigation.

- Symptom: `eshu_dp_projector_stage_duration_seconds{stage="canonical_write"}`
  elevated → likely cause: graph backend write contention or slow Cypher
  execution → check `eshu_dp_canonical_write_duration_seconds` and
  `eshu_dp_neo4j_query_duration_seconds`; inspect `telemetry.SpanCanonicalProjection`
  traces.

- Symptom: projector queue age (`eshu_dp_queue_oldest_age_seconds`) growing →
  likely cause: workers cannot keep up → check `eshu_dp_worker_pool_active`,
  consider raising `Service.Workers`; check `eshu_dp_large_repo_semaphore_wait_seconds`
  if large repos dominate.

- Symptom: phase state missing in `graph_projection_phase_state` → likely cause:
  `PhasePublisher.PublishGraphProjectionPhases` failing silently → check
  `projector runtime stage completed` logs for `stage=canonical_write` error
  fields; check repair queue depth.

- Symptom: entities missing from graph for a repository → likely cause: unmapped
  `entity_type` string dropped in `extractEntities` → add the type to
  `entityTypeLabelMap` and re-project; check `projector runtime stage completed`
  logs for `entity_count=0` on affected generations.

## Anti-patterns specific to this package

- **Branching on backend brand** — do not add `if backend == "nornicdb"` checks
  here. Backend dialect belongs in `internal/storage/cypher` adapters behind the
  `CanonicalWriter` interface.

- **Writing directly to Neo4j/NornicDB drivers** — all graph writes must go
  through `CanonicalWriter.Write`. Direct driver calls bypass instrumentation,
  retry policy, and the backend-neutral contract.

- **Setting `ContentBeforeCanonical` outside local-profile wiring** — this flag
  reverses write order for degraded-backend situations. Setting it in full-stack
  or production wiring breaks the `canonical_nodes_committed` gate that reducer
  edge domains depend on.

- **Adding entity types without schema constraints** — every new entry in
  `entityTypeLabelMap` must have a corresponding Neo4j constraint or index in
  the graph schema. Entries without schema support produce nodes that violate
  the conformance matrix.

## What NOT to change without an ADR

- `CanonicalWriter` interface shape — changing the signature breaks every caller
  and the backend-neutral contract; see
  `docs/docs/adrs/2026-04-22-nornicdb-graph-backend-candidate.md`.
- `graph_projection_phase_state` publish semantics — reducer edge domains gate
  on `canonical_nodes_committed`; removing or deferring the publish breaks
  shared projection ordering.
- `entityTypeLabelMap` entries once a label has graph schema constraints — label
  renames require coordinated graph migration; see
  `docs/docs/adrs/2026-04-17-neo4j-deadlock-elimination-batch-isolation.md` for
  write-order constraints.
