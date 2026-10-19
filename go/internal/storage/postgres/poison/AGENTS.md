# AGENTS.md — storage/postgres/poison guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `liveness_sql.go` (both statements), then `liveness.go`.
3. `../AGENTS.md` for the parent package's queue and lock-order invariants.

## Invariants

- `CountDeadLetters` is read-only: no claim, lock, or mutation. Its cost is
  bounded by the `fact_work_items_dead_letter_poison_idx` partial index; keep
  the `status = 'dead_letter'` equality so the planner can use it.
- `RecoverDeadLetters` runs only when the operator opts into bounded
  auto-retry. Never call it from a default-on path.
- Keep the write-time `target.status = 'dead_letter'` guard on the UPDATE's
  locked relation. Removing it lets a sweep clobber a concurrent reclaim
  (dead_letter -> claimed) back to pending.
- `RecoverDeadLettersQuery` locks its rows in the `poison_locked` CTE, in
  `work_item_id COLLATE "C"` order with a blocking `FOR NO KEY UPDATE`,
  before the UPDATE (#8261): the order every blocking multi-row
  `fact_work_items` locker shares (`SKIP LOCKED` claim and selection lockers
  never wait, so they keep their own order). Keep the `COLLATE "C"` (the default
  collation sorts differently), the lock step's status and budget rechecks,
  and the waiting lock (no `SKIP LOCKED`). Rerun `TestPoisonRecovery*` in the
  parent package after any edit.
- The budget counter stays in the JSONB payload with a `LEAST` cap — do not
  add a schema column for it, and do not widen the candidate CTE past the
  `< maxRecoverAttempts` gate.
- Non-test files never import the parent `postgres` package (cycle: the
  parent's tests and `cmd/reducer` import this package). The external tests
  (`package poison_test`) may import the parent for the generation-liveness
  gauge the detection-gap proof compares against.
- Both query constants stay exported and derived-from, never hand-copied: the
  parent capability latch pins `RecoverDeadLettersQuery` and the EXPLAIN test
  derives its plan from `CountDeadLettersQuery`.

## Common changes

- Budget or batch default changes need a bounded-arm proof update
  (`liveness_integration_live_test.go`, `BoundedArmIncrementsThenZero`): the
  test pins the increment, ceiling, and no-op-at-ceiling behavior.
- Predicate changes need the detection-gap proof (`DetectionGapAndGauge`) and
  the write-time race proof (`liveness_write_time_race_live_test.go`) to keep
  passing unmodified in intent; the live-postgres-readiness runner sets
  `ESHU_POISON_LIVENESS_PROOF_DSN` in CI, while local runs need it against
  local Docker Postgres.
- The proof schema derives from the production bootstrap
  (`provisionPoisonLivenessSchema` in `liveness_integration_helpers_test.go`),
  never from hand-written DDL: a hand-built schema silently falls behind the
  next migration the recovery statement reads (#8302).
