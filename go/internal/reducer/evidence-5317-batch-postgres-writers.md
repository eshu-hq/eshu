# Evidence: #5317 — batch per-row Postgres reducer-fact writers

## Scope

Seven reducer-owned Postgres fact writers (`service_catalog_correlation`,
`security_alert_reconciliation`, `incident_repository_correlation`,
`supply_chain_impact`, `aws_cloud_runtime_drift`, `multi_cloud_runtime_drift`,
`package_correlation`) previously issued one `ExecContext` round-trip per fact
row via `canonicalReducerFactInsertQuery` / the retired
`canonicalVersionedReducerFactInsertQuery`. They now build `reducerFactRow` /
`reducerFactVersionedRow` values and issue one bulk `unnest` `INSERT … ON
CONFLICT (fact_id) DO UPDATE` per `reducerFactBatchSize`-sized chunk via
`reducerBatchInsertFacts` / `reducerBatchInsertVersionedFacts`. The governed
(schema_version-carrying) writers required a new versioned batch sibling so the
inserted rows stay byte-identical.

## Performance Evidence:

- **Backend / version:** Postgres 16 with the `fact_records` schema applied
  (fact_id TEXT PRIMARY KEY; scope_id/generation_id FKs seeded), local instance
  over the loopback (`127.0.0.1`). The path exercised is
  `reducerBatchInsertFacts` vs the pre-#5317 per-row loop over
  `canonicalReducerFactInsertQuery`.
- **Live before/after measurement** (`BenchmarkReducerFactInsertPerRowVsBatched`,
  `reducer_fact_batch_insert_bench_test.go`, `-benchtime=50x`, distinct fact_ids
  so no dedupe collapse):

  | fact rows (N) | per-row loop (before) | batched unnest (after) | speedup |
  |---|---|---|---|
  | 100  | 358.8 ms/op (2000 allocs) | 5.5 ms/op (1592 allocs)  | **~65×** |
  | 1000 | 3.48 s/op (20000 allocs)  | 17.2 ms/op (15102 allocs) | **~203×** |

  The per-row path is dominated by round-trip latency: ~3.6 ms per `ExecContext`
  even on loopback, so N=100 costs ~359 ms and N=1000 ~3.48 s. The batched path
  issues one `unnest` statement (`ceil(N / reducerFactBatchSize=1000)`), so cost
  is near-flat in N. This is a lower bound on the real-world gain — a production
  Postgres reached over the network multiplies the per-row round-trip cost, so
  the per-row/batched gap widens further. Memory is marginally higher per batch
  (the parallel arrays) but allocations are lower.
- **Round-trip-count corroboration:** the committed cost-counting scenarios
  (`go/internal/replay/costcounting/*_cost_test.go`) drive each production writer
  and read the production `eshu_dp_postgres_query_duration_seconds` write
  observation count: a 2-row scope went from **2** observations (per-row) to
  **1** (batched); the budgets ratchet 2→1 and the N+1 negative control (once per
  decision) exceeds the budget of 1.
- **Correctness held constant (output-preserving):** each writer emits
  byte-identical `fact_records` rows to the per-row loop — same
  fact_id/stable_fact_key/schema_version/payload/conflict semantics — asserted
  by per-writer equivalence decoder tests. Rows sharing a `fact_id` are deduped
  to their last occurrence before batching, reproducing the per-row loop's
  last-write-wins final state and avoiding the single-statement ON CONFLICT
  "cannot affect row a second time" error that would otherwise wedge the leased
  projection intent (#2809/#2855 class).
- **No-regression on the drain:** `go test -race ./internal/reducer/
  ./internal/replay/costcounting/` is green; empty-input still issues zero
  statements; error/retry/idempotency semantics are unchanged (each chunk is one
  atomic upsert statement).

## No-Observability-Change:

No instrument, span, log, or status surface is added, removed, or renamed. The
same `eshu_dp_postgres_query_duration_seconds` write histogram observes the
insert path; batching only reduces how many observations a scope produces (fewer
round-trips), which is the intended improvement. Operator-facing telemetry for
reducer fact writes is otherwise identical.
