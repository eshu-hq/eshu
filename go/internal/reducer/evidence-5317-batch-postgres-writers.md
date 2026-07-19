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

- **Backend / version:** Postgres `fact_records` (fact_id TEXT PRIMARY KEY), on
  the reducer projection drain. Instrument:
  `eshu_dp_postgres_query_duration_seconds` observation count
  (`operation="write"`, `store="reducer"`), recorded once per `ExecContext`
  round-trip by `postgres.InstrumentedDB`.
- **Input shape:** the committed cost-counting scenarios drive each production
  writer over 2 distinct-identity decisions in one scope
  (`go/internal/replay/costcounting/*_cost_test.go`).
- **Baseline (per-row loop, before):** N fact rows cost **N** write round-trips
  per scope (O(N)). For the 2-row cost scenarios the measured write-observation
  count was **2** (the exact-equality budgets these tests previously committed).
- **After (bulk unnest, batched):** N rows cost **ceil(N / reducerFactBatchSize)**
  round-trips, `reducerFactBatchSize = 1000` — i.e. **1** statement for any
  scope up to 1000 rows, O(N/1000) overall. The cost budgets are ratcheted from
  2 to **1** and the tests re-run green with the standard N+1 negative control
  (calling the writer once per decision drives 2 observations and exceeds the
  budget of 1), proving the batched path issues one round-trip where the per-row
  path issued two.
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
