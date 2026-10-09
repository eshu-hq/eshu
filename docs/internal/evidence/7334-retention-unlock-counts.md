# #7334 sections 2-3: retention plans unlocked, then re-locks

Scope: `PruneSupersededGenerations`, `selectPrunableCandidates` and
`selectCandidates` in `go/internal/storage/postgres/generation_retention.go`;
`countRows`, the row-limit selection rules, the own-fact pre-screen and the
selection re-lock in
`go/internal/storage/postgres/generation_retention_events.go`; the pre-screen
and targeted-lock statements in
`go/internal/storage/postgres/generation_retention_sql.go`. Section 1 (the
candidate selection rewrite) is `7334-generation-retention-selection.md`;
this is the separate PR that doc names. The terms-cascade step stays out of
scope.

Authority: the arbiter ruling for this drive (fix 1: savepoint rollback,
unlocked pre-screen and counts, targeted re-lock in deterministic order,
mandatory recount under the lock; fix 2: stateless own-fact pre-screen; fix
3: `row_limit_own_rows` reason split), recorded in the PR.

## The defect (baseline)

Every pass held the candidate selection's scope and generation row locks
from the candidate query through the row counts, the re-checks and the
prune to the commit. A production-shaped log of 31 cycles (issue comment
2026-10-08) showed the same 14 own-rows over-limit generations skipped
every cycle, `count_rows` at 26-29s per cycle with the scope lock held for
almost the whole cycle, and about 27% of loop time spent counting without
progress. Every skip reported the same `row_limit` reason whether it was a
permanently unprunable generation or a transiently full batch.

## The fix (after)

Each pass now selects inside the selection savepoint, rolls it back
immediately, and plans unlocked: the own-fact pre-screen drops generations
whose own `fact_records` rows already exceed `BatchRowLimit` (sound: fact
rows are a subset of the rows outside the changed-since ledger, and every
count arm is non-negative), the row count sizes the rest, and the capped
re-check loop settles the provisionally selected set. The pass then
re-locks only that set with the targeted query — set form, deterministic
`(scope_id, generation_id)` order, `SKIP LOCKED` — recounts once under the
new locks, and prunes. A re-lock miss is silently dropped (the holder
resolves the race; a later pass retries). `ScopeLockHold` now spans the
re-lock to the commit and is zero when the pass locks nothing.

Skip reasons split three ways: `row_limit` (fits alone, batch full:
transient), `row_limit_own_rows` (own rows outside the ledger exceed the
limit: permanent), `row_limit_ledger` (unchanged).

## Measurements (throwaway shim, deleted after)

`EXPLAIN (ANALYZE, BUFFERS)` on a seeded fixture (60,000 fact rows plus
content/ledger rows, local Postgres 18), full 30-arm count versus the
grouped own-fact pre-screen:

- Full count: execution 381.309 ms, 231,734 shared buffers. Content arms
  dominate (`content_file_references` GroupAggregate+Sort ~88 ms / ~60k
  buffers; files/entities/secret-lines ~40 ms batches; doomed CTEs
  ~27-30 ms).
- Pre-screen: execution 15.461 ms, 1,346 shared buffers — 24.7x faster,
  172x fewer buffers. (The shim ran unanalyzed and seq-scanned; the
  committed `PrescreenProbesFactIndex` guard pins the indexed path on an
  analyzed table.)

## Proofs (committed)

- `TestGenerationRetentionFirstCountRunsWithoutScopeLocksLive`: a second
  session holds no candidate scope lock while the first full count runs
  (was RED: both scopes locked).
- `runRetentionCycleUnderContention` (probe scale test): a contending fact
  insert now waits out only the narrow window (waited 423 ms against a
  419 ms hold) instead of the whole count.
- `TestGenerationRetentionPrescreenSkipsOverFactGenerationsLive` (mixed
  over/under fixture through the real store) plus the fake
  `...KeepsOverFactGenerationsOutOfTheCount` (count statements never name
  a pre-screened generation) and `...SkipsFullCountWhenAllOver` (no full
  count runs at all when every candidate is pre-screened out).
- `TestGenerationRetentionTargetedLockPlanShapeLive`: LockRows node plus
  the ORDER BY / SKIP LOCKED clauses, each with a seeded variant that
  makes its guard fail.
- `TestGenerationRetentionTargetedLockEvalPlanQualDropsRacedMembersLive`:
  a deleted or reactivated member is dropped while a bystander member of
  the same set is still locked.
- `TestGenerationRetentionRelockDropsMissedMembersAndPrunesTheRest`
  (fake): a partial re-lock miss prunes the locked subset with no skip
  for the dropped member; full-miss behavior stays pinned by the
  (c) TakenElsewhere proofs.
- Reason split: `TestGenerationRetentionOwnRowOverLimitReportsPermanentReason`
  (was RED) and `TestGenerationRetentionBatchFullKeepsTransientReason`
  (guard, green throughout).
- Full `TestGenerationRetention` suite (fake + live, incl. selection,
  recheck, ledger, grandchild, hard-ceiling and EPQ proofs): green,
  `go test ./internal/storage/postgres/ -run TestGenerationRetention`,
  93.5s on local Postgres 18.
