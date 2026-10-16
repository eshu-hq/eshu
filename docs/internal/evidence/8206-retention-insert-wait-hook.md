# #8206 Retention Insert-Wait Test Hook Evidence

## Scope

`TestGenerationRetentionChunkConcurrentInsertWaitsLive` flaked because it
started five fact inserts at the same moment as the prune. When an insert's
foreign-key check took `FOR KEY SHARE` on the scope and generation rows
first, the prune's `FOR UPDATE OF generation, scope SKIP LOCKED` skipped the
generation on purpose and the test saw `Skipped[row_limit_chunked] = 0`.

The fix is test-only ordering. The store's test hook field moves into an
embedded, unexported `generationRetentionTestHooks` struct
(`generation_retention_policy.go`). `relockChunkedBatch` calls a new
`afterChunkTargetLocked` hook once the chunk target is locked and before its
page delete. Production never sets either hook. No retention SQL, lock
strength, lock order, skip semantics, batch size, worker or retry behavior
changes.

## No-Regression Evidence: one nil check per chunked re-lock

- Baseline: `067f59bf3` (`origin/main`). The before runs below were taken at
  `711ed3122`; no commit between the two touches
  `go/internal/storage/postgres/generation_retention*`. The only product-path
  change is one `if lockedChunk != nil && s.afterChunkTargetLocked != nil`
  check, run once per chunked re-lock (at most once per prune pass). It is
  not a per-row or per-statement path, so no benchmark applies.
- Backend: disposable `postgres:18` (PostgreSQL 18.6) container on a random
  host port, migrated schema through `openGenerationRetentionMigratedSchema`.
- Input shape: one superseded generation with 1,500 facts, `BatchRowLimit`
  1,000, five concurrent single-row inserts.
- Before (diagnosis run at base): 49 of 50 `-count=50` runs passed, 1 failed
  with `Skipped[row_limit_chunked] = 0, want 1`. A probe that opened one
  uncommitted insert in `beforeTargetedLock` gave `Skipped=map[]`, 0 facts
  pruned, 1,500 facts left.
- After: `-count=50` passed 50 of 50 (exit 0); `-race -count=5` passed 5 of 5
  (exit 0). Each run saw 0 tagged inserts before the hook started them,
  then exactly 5 lock-blocked inserts in `pg_stat_activity`, `RowsPruned[fact_records] = 1000`,
  `Skipped[row_limit_chunked] = 1`, and 505 facts left. The whole
  `-run 'GenerationRetention'` suite passed (78 tests, exit 0).
- Why safe: the hook seam is nil in production and adds no query, lock or
  allocation. The skip-on-lock-miss behavior stays as documented in
  `go/internal/reducer/maintenance/retention/runner.go` and stays covered by
  `TestGenerationRetentionChunkLockMissDropsSilentlyLive`.

## No-Observability-Change: no runtime signal changes

No metric, span, log or status field changes. The existing retention signals
(`eshu_dp_generation_retention_skipped_total`, `_rows_pruned_total`,
`_scope_lock_hold_seconds` and the per-cycle log) report the chunk path
exactly as before, because the product path does the same work.
