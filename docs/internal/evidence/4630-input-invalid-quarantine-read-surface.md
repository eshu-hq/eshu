# #4630 — Durable input_invalid quarantine read surface

Adds a durable per-fact quarantine table (`reducer_input_invalid_facts`) so an
operator can query which facts the reducer skipped during typed-payload decode
for a missing or null required field, instead of only seeing the aggregate
`eshu_dp_reducer_input_invalid_facts_total` rate and a structured log line. The
reducer's existing quarantine choke point (`recordQuarantinedFacts`,
`go/internal/reducer/factschema_decode.go`) now also best-effort persists each
quarantined fact through an optional `QuarantinedFactWriter`
(`go/internal/reducer/quarantine_writer.go`), stashed on the execution context
once per intent by `Service.executeWithTelemetry`
(`go/internal/reducer/service.go`). The write goes through
`ReducerInputInvalidFactStore.WriteQuarantinedFacts`
(`go/internal/storage/postgres/reducer_input_invalid_facts.go`), a batched
multi-row `INSERT ... ON CONFLICT (scope_id, generation_id, fact_id,
missing_field) DO NOTHING`. A new bounded, scoped read
(`POST /api/v0/admin/input-invalid-facts/query`,
`go/internal/query/admin_input_invalid_facts.go`; MCP mirror
`list_reducer_input_invalid_facts`) exposes it.

## No-Regression Evidence:

- **Change shape on the existing hot path (`recordQuarantinedFacts`):** the
  function's pre-existing behavior (per-fact counter increment + structured
  error log) is unchanged; it now ALSO builds one `QuarantinedFactRecord` per
  quarantined fact and calls `persistQuarantinedFacts`. For the overwhelmingly
  common case — zero quarantined facts in the intent — `recordQuarantinedFacts`
  returns at its existing early `len(quarantined) == 0` check before any new
  code runs, so a healthy scope generation with no malformed facts pays zero
  added cost. `Service.executeWithTelemetry`'s only unconditional addition is
  `execCtx = WithQuarantineWriter(execCtx, s.QuarantineWriter)`, a single
  `context.WithValue` wrap (one pointer-sized allocation) on every claimed
  intent, not a Cypher, graph-write, queue, lease, or batching change.
- **Change shape on the write itself:** `persistQuarantinedFacts` batches every
  quarantined fact from one intent into ONE call to
  `WriteQuarantinedFacts`, which issues one bounded multi-row `INSERT` per
  250-row batch (`reducerInputInvalidFactBatchSize`, mirroring the existing
  `graphProjectionPhaseStateBatchSize` convention) — a single round trip per
  intent for the normal case (intents quarantine at most a handful of facts,
  never hundreds), not an N+1 per-fact write.
- **Baseline:** the #4630 golden-corpus gate on the NornicDB backend
  (`gc4630`, 20-repo corpus, unique ports) —
  `418 pass, 0 required-fail, 0 advisory-warn`,
  `PASS: B-7 golden corpus gate green (elapsed 34s, budget ceiling 1800s)`,
  including a live `mcp:list_reducer_input_invalid_facts` shape assertion
  (`"items" has 0 results`) with no B-12 snapshot drift beyond the additive new
  row.
- **After:** the golden corpus's fixture repos decode cleanly (no malformed
  facts), so every reducer intent in that run takes the unchanged
  zero-quarantine early-return path; the new write path is exercised
  separately by `TestReducerInputInvalidFactStoreLive` (real Postgres) which
  proves the batched `INSERT ... ON CONFLICT DO NOTHING` writes exactly the
  expected rows once and stays at that row count after an identical replay
  (idempotent), then that FK-cascading the owning `scope_generations` row
  deletes the quarantine rows.
- **Read path bound:** `POST /api/v0/admin/input-invalid-facts/query` requires
  `scope_id`, `generation_id`, `limit` (<=500), and `timeout_ms` (<=30s),
  reads a single indexed `(scope_id, generation_id, domain, fact_kind,
  decided_at DESC)` range, over-fetches by exactly one row to set `truncated`,
  and is scoped by the caller's granted repository/scope ids before the store
  is ever called (`TestListInputInvalidFactsScopedUngrantedScopeSkipsStore`) —
  never an unbounded or whole-table scan.

## No-Observability-Change:

Not applicable — this change adds new telemetry rather than preserving an
existing contract unchanged:

- `eshu_dp_reducer_input_invalid_fact_write_batch_size` (histogram): rows per
  batched durable write.
- `eshu_dp_reducer_input_invalid_facts_committed_total` (counter): rows
  successfully committed.
- `eshu_dp_reducer_input_invalid_fact_write_errors_total` (counter, labeled
  `reason`): failed batched writes; the write is best-effort and this counter
  is the only operator signal of a durable-write outage (the fact remains
  correctly quarantined — counted and logged — either way).
- `eshu_dp_query_input_invalid_facts_duration_seconds` (histogram) and
  `eshu_dp_query_input_invalid_facts_errors_total` (counter, labeled
  `reason`): the bounded read's duration and failure classification.

The pre-existing `eshu_dp_reducer_input_invalid_facts_total` counter and the
"reducer input_invalid fact quarantined" structured error log are unchanged in
shape and continue to fire regardless of whether the new durable write
succeeds, fails, or is disabled (nil `Service.QuarantineWriter`).

## Concurrency

`persistQuarantinedFacts` never returns an error to its caller: a durable-write
failure is logged (`slog.ErrorContext`) and counted
(`eshu_dp_reducer_input_invalid_fact_write_errors_total`), then swallowed. This
is proven by `TestRecordQuarantinedFactsWriteFailureIsNonFatal`
(`go/internal/reducer/quarantine_writer_test.go`), which injects a write error
and asserts `recordQuarantinedFacts` still returns the correct quarantine count
with no panic or propagated error. Idempotent replay under concurrent or
retried reduction is proven against real Postgres by
`TestReducerInputInvalidFactStoreLive`
(`go/internal/storage/postgres/reducer_input_invalid_facts_live_test.go`): the
same batch written twice (as a retried intent would) produces the same row
count, because `ON CONFLICT (scope_id, generation_id, fact_id, missing_field)
DO NOTHING` converges on the natural key rather than erroring or duplicating.
