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
4. `go/internal/storage/postgres/facts.go` — `upsertFacts`,
   `deduplicateEnvelopes`, `sanitizeJSONB`; understand the batching and
   deduplication constraints before changing fact write paths
5. `go/internal/storage/postgres/schema.go` — `BootstrapDefinitions`,
   `ApplyDefinitions`; DDL ordering and idempotency rules

## Invariants this package enforces

- **Idempotency** — all INSERT paths use `ON CONFLICT DO NOTHING` or
  `ON CONFLICT DO UPDATE`; schema DDL uses `IF NOT EXISTS`. Do not add
  non-idempotent INSERTs or CREATE TABLE without IF NOT EXISTS.
- **Fact deduplication before batching** — `upsertFacts` calls
  `deduplicateEnvelopes` before each batch to prevent `SQLSTATE 21000` on
  `ON CONFLICT DO UPDATE` when the same `fact_id` appears twice in one batch
  (`facts.go:192`).
- **JSONB sanitization** — `sanitizeJSONB` removes ` ` escape sequences
  and raw control bytes before every fact INSERT (`facts.go:435`). Skipping
  this causes Postgres errors on repositories with binary or non-UTF-8 content.
- **Ack atomicity** — `ProjectorQueue.Ack` wraps four SQL statements in a
  single transaction (`projector_queue.go:315`). If any step fails, the
  transaction rolls back. Always pass a `SQLDB` or `InstrumentedDB(SQLDB)` to
  `NewProjectorQueue`; a bare `ExecQueryer` without `Beginner` will fail.
- **Lease fencing** — `ProjectorQueue.Heartbeat` and `WorkflowControlStore`
  claims check `lease_owner` on UPDATE. A zero `RowsAffected` returns
  `ErrProjectorClaimRejected` or `ErrWorkflowClaimRejected`. Callers must stop
  processing on these errors and must not retry the ack.
- **NornicDB semantic gate** — `ReducerQueue.Claim` blocks
  `semantic_entity_materialization` while source-local projection is in-flight
  when the NornicDB gate parameter is true. Do not remove or bypass this gate
  without an ADR.
- **Schema ordering** — tables with foreign key constraints must appear after
  their referenced tables in `bootstrapDefinitions`. Current FK dependencies:
  `graph_projection_phase_state` → `ingestion_scopes` + `scope_generations`.

## Common changes and how to scope them

- **Add a new Postgres store** → implement against `ExecQueryer`; add a
  `New*Store(db ExecQueryer)` constructor; add a `*SchemaSQL()` function
  returning idempotent DDL; register it in `BootstrapDefinitions` in `schema.go`
  with the correct position in the slice; wrap with `InstrumentedDB` in `cmd/`
  wiring for observability.

- **Add a new queue domain to ReducerQueue** → add the domain constant in
  `internal/reducer`; extend the `domain = $2` filter handling in
  `ReducerQueue.Claim`; add tests for claim, ack, and retry paths.

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

- Symptom: `ErrProjectorClaimRejected` or `ErrReducerClaimRejected` in logs →
  lease expired before ack; increase `LeaseDuration` or reduce projection time;
  check `eshu_dp_projector_stage_duration_seconds` for slow phases.

- Symptom: `dead_letter` items accumulating → check `failure_class` in
  `fact_work_items`; replay via `RecoveryStore` after root-cause investigation.

- Symptom: `graph_projection_phase_state` rows missing for a scope generation →
  projector `publish_phases` stage failed; check `GraphProjectionPhaseRepairQueueStore`
  depth; check projector structured logs for `stage=canonical_write` error fields.

- Symptom: `SQLSTATE 22P05` or `SQLSTATE 22P02` on fact INSERT → non-UTF-8 or
  binary payload; `sanitizeJSONB` in `facts.go:435` should handle this; check
  whether the repo emits raw binary in fact payloads.

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
