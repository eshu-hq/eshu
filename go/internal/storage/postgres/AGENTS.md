# AGENTS.md — storage/postgres guidance for LLM assistants

## Read first

1. `go/internal/storage/postgres/README.md` — pipeline position, store
   inventory, queue lifecycle, and operational notes
2. `go/internal/storage/postgres/db.go` — `ExecQueryer`, `Transaction`,
   `Beginner`, `SQLDB`, `SQLTx`; understand the interface hierarchy before
   touching any store
3. `go/internal/storage/postgres/projector_queue.go` — `ProjectorQueue.Claim`
   and `Ack`; the four-step atomic ack transaction is the most sensitive path
   in this package
4. `go/internal/storage/postgres/projector_queue_sql.go` — projector claim,
   stale-generation coalescing, duplicate-lease reclaim, and lifecycle SQL
5. `go/internal/storage/postgres/facts.go` — `upsertFacts`,
   `deduplicateEnvelopes`, `sanitizeJSONB`; understand the batching and
   deduplication constraints before changing fact write paths
6. `go/internal/storage/postgres/status_queries.go` — status aggregate SQL,
   including fact queue and shared projection domain backlog
7. `go/internal/storage/postgres/schema.go` — `BootstrapDefinitions`,
   `ApplyDefinitions`; DDL ordering and idempotency rules

## Invariants this package enforces

- **Idempotency** — all INSERT paths use `ON CONFLICT DO NOTHING` or
  `ON CONFLICT DO UPDATE`; schema DDL uses `IF NOT EXISTS`. Do not add
  non-idempotent INSERTs or CREATE TABLE without IF NOT EXISTS.
- **Fact deduplication before batching** — `upsertFacts` calls
  `deduplicateEnvelopes` before each batch to prevent `SQLSTATE 21000` on
  `ON CONFLICT DO UPDATE` when the same `fact_id` appears twice in one batch
  (`facts.go:206`).
- **Freshness de-dupe covers in-flight generations** —
  `CommitScopeGeneration` must compare the incoming `FreshnessHint` with the
  newest same-scope `pending` or `active` generation. Restricting the check to
  `ingestion_scopes.active_generation_id` lets local polling recommit the same
  snapshot while projection is still in flight, which creates avoidable
  supersession churn. Do not include `failed` generations in this skip path; a
  failed first projection must remain retryable by a later snapshot.
- **JSONB sanitization** — `sanitizeJSONB` removes `\u0000` escape sequences
  and raw control bytes before every fact INSERT. Skipping this causes Postgres
  errors on repositories with binary or non-UTF-8 content.
- **Ack atomicity** — `ProjectorQueue.Ack` wraps four SQL statements in a
  single transaction (`projector_queue.go:105`). If any step fails, the
  transaction rolls back. Always pass a `SQLDB` or `InstrumentedDB(SQLDB)` to
  `NewProjectorQueue`; a bare `ExecQueryer` without `Beginner` will fail.
- **Lease fencing** — `ProjectorQueue.Heartbeat` and `WorkflowControlStore`
  claims check `lease_owner` on UPDATE. A zero `RowsAffected` returns
  `ErrProjectorClaimRejected` or `ErrWorkflowClaimRejected`. Callers must stop
  processing on these errors and must not retry the ack.
- **Projector scope ordering** — `ProjectorQueue.Claim` must preserve one
  active source-local generation per `scope_id`. Keep the oldest-ready-row
  subquery with `FOR UPDATE SKIP LOCKED`; without it, parallel claimers can skip
  a locked older row and start a newer generation for the same repository.
  Expired `claimed` or `running` rows must stay ahead of ordinary pending rows
  in the claim ordering, or stale leases remain overdue while newer generations
  drain. Keep the stale duplicate reclaim CTEs in the claim path: they demote
  expired same-scope siblings to `retrying` when another live or newly claimed
  sibling owns the scope. Keep the `ProjectorQueue.Claim`
  stale-generation coalescing path (`projector_queue.go:74`) and the companion
  CTEs together; they move older same-scope projector rows and pending or
  failed `scope_generations` to `superseded` so durable snapshot history
  remains available without reprocessing obsolete local polling generations or
  reporting superseded terminal failures as current health.
  Keep the `ProjectorQueue.Heartbeat` supersede check with that claim behavior:
  live older same-scope generations must return `projector.ErrWorkSuperseded`
  once a newer generation is visible, or local polling can spend minutes writing
  graph state that will be immediately obsolete.
- **NornicDB semantic gate** — `ReducerQueue.Claim` blocks
  `semantic_entity_materialization` while source-local projection is in-flight
  when the NornicDB gate parameter is true. Do not remove or bypass this gate
  without an ADR.
- **Status domain backlog includes shared projection.** `StatusStore` merges
  `fact_work_items` backlog with pending `shared_projection_intents` so
  `/admin/status` remains `progressing` until reducer-owned shared edges are
  graph-visible. Do not remove that union when editing `status.go`.
- **Schema ordering** — tables with foreign key constraints must appear after
  their referenced tables in `bootstrapDefinitions`. Current FK dependencies:
  `graph_projection_phase_state` → `ingestion_scopes` + `scope_generations`.

## Common changes and how to scope them

- **Add a new Postgres store** → implement against `ExecQueryer`; add a
  `New*Store(db ExecQueryer)` constructor; add a `*SchemaSQL()` function
  returning idempotent DDL; register it in `BootstrapDefinitions` in `schema.go`
  with the correct position in the slice; wrap with `InstrumentedDB` in `cmd/`
  wiring for observability.

- **State-attribute decoding or flattening** → edit
  `tfstate_drift_evidence_state_row.go`. `stateRowFromCollectorPayload`
  (`tfstate_drift_evidence_state_row.go:21`) decodes the collector payload and
  calls `flattenStateAttributes` (same file, line 71) to produce the flat
  dot-path `map[string]string`. The dot-path encoding MUST stay byte-identical
  to `ctyValueToDriftString` in
  `go/internal/parser/hcl/terraform_resource_attributes.go`; the classifier's
  value-equality check in `go/internal/correlation/drift/tfconfigstate/classify.go`
  fires across both sides. Add a cross-package regression test when changing any
  encoding rule. Singleton repeated blocks arrive from the state collector as
  `[]any` of length 1 whose first element is `map[string]any`;
  `flattenStateAttributes` unwraps the outer array and recurses into the object
  so paths like `versioning.enabled` align with the parser-emitted form.

- **Parser-entry bridging (config side)** → edit
  `tfstate_drift_evidence_config_row.go`. `configRowFromParserEntry`
  (`tfstate_drift_evidence_config_row.go:22`) maps one `terraform_resources`
  JSON entry from the HCL parser into a `tfconfigstate.ResourceRow`. The
  `attributes` field is already flat dot-path from the parser; this function
  copies it to `ResourceRow.Attributes`. `unknown_attributes` is a JSON array
  of dot-path strings; it becomes `ResourceRow.UnknownAttributes` so
  `classifyAttributeDrift` can skip non-literal expressions.
  Note: `loadPriorConfigAddresses` (see below) also calls
  `configRowFromParserEntry` to extract addresses from prior-generation
  parser facts. The same dot-path address-space contract applies to both the
  current-config path and the prior-config walk.

- **Prior-config walk for `removed_from_config`** → edit
  `tfstate_drift_evidence_prior_config.go`. `loadPriorConfigAddresses`
  (`tfstate_drift_evidence_prior_config.go:45`) queries prior repo-snapshot
  generations bounded by `PostgresDriftEvidenceLoader.PriorConfigDepth`
  (default 10, set from `ESHU_DRIFT_PRIOR_CONFIG_DEPTH`). It returns the
  address set that `mergeDriftRows` uses to set `PreviouslyDeclaredInConfig`
  on state-only addresses. When changing depth semantics, also update the
  `defaultPriorConfigDepth` constant in the same file and the
  `parsePriorConfigDepth` helper in `go/cmd/reducer/config.go`.

- **Add a new queue domain to ReducerQueue** → add the domain constant in
  `internal/reducer`; extend the `domain = $2` filter handling in
  `ReducerQueue.Claim`; add tests for claim, ack, and retry paths.

- **Add a new enqueue-only call site for `ReducerQueue`** → construct the
  struct directly (`ReducerQueue{db: s.db}`) without `LeaseOwner` or
  `LeaseDuration`. Both fields remain NULL on insert per
  `enqueueReducerBatchPrefix`; the enqueue SQL never reads them. The
  internal `validateEnqueue` runs without lease fields and `validateClaim`
  extends it with the lease-owner fence used by `Claim`, `Heartbeat`,
  `Ack`, and `Fail`. Do not invent a parallel ReducerQueueEnqueuer port —
  `projector.ReducerIntentWriter` already provides the narrower
  consumer-side interface (`internal/projector/runtime.go`).

- **Add a new fact kind or column** → update `upsertFactBatch` column list and
  `columnsPerFactRow`; update `scanFactEnvelope`; update the schema DDL; add a
  migration if the column is non-nullable without a default.

- **Add a new graph projection phase** → add the phase constant in
  `internal/reducer`; batch-upsert it via `GraphProjectionPhaseStateStore`; add
  a matching readiness lookup path if reducer domains gate on it.

- **Add Postgres telemetry** → wrap the `ExecQueryer` with `InstrumentedDB`;
  set `StoreName` to a short descriptive label; the metric
  `eshu_dp_postgres_query_duration_seconds{store=...,operation=...}` is emitted
  automatically.

## Failure modes and how to debug

- Symptom: claim latency high (`eshu_dp_postgres_query_duration_seconds{store="queue"}`)
  → check index coverage on `fact_work_items(stage, status, visible_at,
  claim_until)` and `FOR UPDATE SKIP LOCKED` contention.

- Symptom: multiple `projector` rows are `running` for the same `scope_id` →
  check the oldest-ready-row guard in `ProjectorQueue.Claim`. Same-scope
  duplicate running rows can fence pending generations and make local progress
  look stalled even when processes are alive. If overdue claims stay visible
  while pending rows continue to move, check that expired-lease priority still
  precedes `updated_at` ordering and that stale duplicate reclaim still demotes
  expired siblings to `retrying`.

- Symptom: `ErrProjectorClaimRejected` or `ErrReducerClaimRejected` in logs →
  lease expired before ack; increase `LeaseDuration` or reduce projection time;
  check `eshu_dp_projector_stage_duration_seconds` for slow phases.

- Symptom: `dead_letter` items accumulating → check `failure_class` in
  `fact_work_items`; replay via `RecoveryStore` after root-cause investigation.

- Symptom: `graph_projection_phase_state` rows missing for a scope generation →
  projector `publish_phases` stage failed; check `GraphProjectionPhaseRepairQueueStore`
  depth; check projector structured logs for `stage=canonical_write` error fields.

- Symptom: `SQLSTATE 22P05` or `SQLSTATE 22P02` on fact INSERT → non-UTF-8 or
  binary payload; `sanitizeJSONB` should handle this; check whether the repo
  emits raw binary in fact payloads.

- Symptom: `SQLSTATE 21000` on fact INSERT → duplicate `fact_id` in a batch;
  check `deduplicateEnvelopes` is being called; should not happen in normal
  operation.

## Anti-patterns

- **Do not bypass `deduplicateEnvelopes`** when calling `upsertFactBatch`
  directly. Duplicate `fact_id` values in a single multi-row INSERT trigger
  `SQLSTATE 21000`.
- **Do not use raw SQL string building** when adding new stores. Use parameterized
  queries (`$1`, `$2`, ...) exclusively to prevent injection.
- **Do not hold long transactions** across graph writes. The projector ack
  transaction is bounded to four SQL statements; do not add graph or network
  calls inside it.
- **Do not add `if backend == "nornicdb"` branches** here. Backend-specific
  queue gate logic is isolated to `ReducerQueue.Claim`'s parameterized gate
  (`reducer_queue.go`). New backend gates must go in the same parameterized
  pattern.
- **Do not skip `WorkflowControlStore` lease fencing**. Always check the
  returned error from claim mutations; silently ignoring `ErrWorkflowClaimRejected`
  causes split-brain workflow state.
- **Do not raise `contentWriterBatchConcurrencyAutoCap` or
  `contentWriterBatchConcurrencyCap` without re-running the pool-budget
  math** (`content_writer_batch.go`). Peak Postgres connection demand is
  `ESHU_PROJECTOR_WORKERS * batch_concurrency`; the auto cap is set
  against the 30-conn default pool from `internal/runtime/data_stores.go`.
  Raising one without raising the other can starve collector, status, and
  heartbeat paths.
- **Do not re-introduce per-call `os.Getenv` reads in `ContentWriter`.**
  The env override is resolved once in `NewContentWriter`; per-call reads
  let a long-running ingester pick up live env changes the operator never
  expected to be hot-reloaded.
- **Do not assert positional `db.execs[i]` order on the entity-batch
  path.** Entity batches fan out through `runConcurrentBatches` and
  arrive in non-deterministic order. Assert on the sorted multiset of
  batch sizes or on the per-batch query shape instead.
- **Do not diverge the dot-path encoding between `coerceJSONString` /
  `flattenStateAttributes` (`tfstate_drift_evidence_state_row.go:21`) and
  `ctyValueToDriftString`
  (`go/internal/parser/hcl/terraform_resource_attributes.go`).** The
  classifier's value-equality check in
  `go/internal/correlation/drift/tfconfigstate/classify.go` compares strings
  produced by both sides; silent divergence causes false-positive or
  false-negative `attribute_drift` detection without test failures in either
  package alone. Add a cross-package test before changing any encoding rule.
  The same address-space contract governs `loadPriorConfigAddresses`: it
  calls `configRowFromParserEntry` on prior-generation facts, so the address
  strings it produces must match what the current-config path produces. A
  divergence silently misclassifies resources as `added_in_state` instead of
  `removed_from_config`.

## What NOT to change without an ADR

- `fact_work_items` table schema (columns, indexes, conflict keys) — the projector
  and reducer queue claim queries are tightly coupled to this schema; changes
  require coordinated migration and claim query updates.
- `graph_projection_phase_state` schema and phase semantics — reducer edge
  domains gate on specific phase values; changing phase names or semantics
  breaks the readiness contract across `internal/reducer`.
- `ReducerQueue.Claim` NornicDB semantic gate — its presence and activation
  condition are evidence-backed; see
  `docs/docs/adrs/2026-04-22-nornicdb-graph-backend-candidate.md`.
- `BootstrapDefinitions` ordering — FK constraints enforce ordering; reordering
  without verifying all FK dependencies will break bootstrap in fresh
  deployments.
