# Evidence: #7929 — the reachability poll's per-run max probe re-reads the intent heap

Scope: the pending-code-reachability loader's candidate statement
(`listPendingCodeReachabilityInputsSQL` in
`go/internal/storage/postgres/code/reachability/loader_candidates_sql.go`).
The fix is one index, migration
`178_shared_projection_intents_acceptance_completed_idx.sql`:
`shared_projection_intents (scope_id, acceptance_unit_id, source_run_id,
generation_id, projection_domain, completed_at) WHERE completed_at IS NOT
NULL`. No Go SQL changes. Quiescence, projector-claim, and reducer-drain
theories are unproven against their per-call gaps and untouched.

This is a **Prove-The-Theory-First** record. The theory was re-derived on a
scratch container (not trusted blind): the BEFORE plan was reproduced there
at the issue's magnitude (~465k buffers), then the candidate index was
built and the AFTER plan measured on the same corpus and storage state. No
DDL ran on production. Repository ids here are scratch placeholders.

## The defect

Each ready run's `CROSS JOIN LATERAL (SELECT max(intent.completed_at) ...)`
filters on `(scope_id, acceptance_unit_id, source_run_id, generation_id)`,
a two-value `projection_domain` IN list, and `completed_at IS NOT NULL`.
The migration-008 `acceptance_lookup_idx` lacks `generation_id` and
`completed_at`, so every probe degrades to a bitmap heap walk over the
run's whole completed-intent set with `generation_id` as a heap Filter.

Measured on the scratch container (PG 18-alpine, defaults, 660,000
completed `code_calls`/`inheritance_edges` rows across 40 ready runs,
watermarks current at epoch 4 so the poll returns 0 rows, VACUUM
ANALYZEd, exact shipped statement at LIMIT 10, warm runs):

- `Bitmap Heap Scan`, 16,500 rows x 40 loops, `Heap Blocks: exact=463160`,
  `Buffers: shared hit=464858`, `Execution Time: 638 to 696 ms`, 0 rows.

## After

Same container, same corpus, same storage state, VACUUM ANALYZEd after
the concurrent build, three warm runs of the same statement:

- `Index Only Scan using
  shared_projection_intents_acceptance_completed_idx`, `Heap Fetches: 0`,
  `Buffers: shared hit=1390`, `Execution Time: 66 to 67 ms`, 0 rows.

Eligible work is unchanged: at a stale verdict epoch both shapes return
the same 40 runs byte-identical (`diff` clean), and at the current epoch
both return 0 rows. The `TestCodeReachabilityLoaderGate*` live battery
(gate skips incomplete runs, legacy row-set differential, load-bearing
predicates) passes unmodified, plus the new
`TestCodeReachabilityLoaderUsesCompletedIntentsIndex` live proof (index
valid and ready, plan uses it at 200k-row fixture scale, rows equal the
pre-#7547 oracle) and the hermetic migration-shape test.

## Cost

Index: 5,544 kB on the 660k-row corpus, built CONCURRENTLY in 1.64 s.
The corpus reuses a small timestamp pool so btree dedup flatters the
size; budget one narrow six-column entry per completed intent row.
Write path, from `EXPLAIN (ANALYZE, WAL)` over 10,000-row
completed-intent inserts, one arm each with CHECKPOINT before each:
50,799 WAL records and 5,351,382 bytes without, against 61,062 records
and 6,386,325 bytes with, about 1.03 extra WAL records and 103 extra
bytes per row. Completing an intent (UPDATE of `completed_at`) pays the
entry once the row qualifies for the partial predicate. The migration-008
lookup index is kept (no DROP): pending-acceptance readers still order by
`created_at` on its key, and dropping it needs its own owner review.

## What this does not fix

The remaining ~66 ms is the aggregate visiting one index-only entry per
completed intent of each run (16,500 x 40 loops): `max()` over a
two-value IN list does not short-circuit to a backward limit scan. A
further rewrite of the probe (one `LIMIT 1` scan per domain) is a query
change for a follow-up, not this index. Poll cadence (5 s default) is
unchanged.

## Observability

No-Observability-Change: the runner's existing `code reachability
projection completed` log already carries input count, row count, and
`duration_seconds` per cycle, and the query runs under the existing
`InstrumentedDB` Postgres spans. Operators watch poll cost through those;
no new metric, span, log key, or knob.
