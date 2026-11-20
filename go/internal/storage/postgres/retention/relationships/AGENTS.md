# AGENTS.md — storage/postgres/retention/relationships

## Read first

1. `README.md` and `doc.go` in this directory.
2. `sql.go`: every statement and the comments that state why it is shaped so.
3. `../../AGENTS.md` for Postgres storage conventions.
4. `../../repository_retirements_purge_sweep.go`: the #10820 purge this sweep's
   lock order must be read against, and
   (#10823) for the measured
   plans, WAL figures and mutant results.

## Invariants

- Rule A is the only retention key: no `scope_generations` row, not any scope's
  `active_generation_id`, no `active` generation row. Do not widen it.
- Every statement that takes rows to delete (the evidence and candidate
  picks, the generation row delete) carries all three predicates in its own
  `WHERE`. `= ANY($1)` only narrows. Never replace the predicates with trust in
  the id list (`TestDeleteStatementsReassertEveryPredicate`, and the live
  adversarial-array test).
- The statements take the classification statement's claimed ids, never the
  batch input: an id whose generation row another session holds must not be
  deleted (`TestDeletesReceiveOnlyTheClassifiedIDs`).
- Delete order is evidence, candidates, generation row last, one transaction.
  The #10820 purge deletes in the same order, but the lock orders still cross:
  the batch locks the generation row first, the purge last. Do not reorder.
- The batch never waits for a row lock. Every row lock is `FOR UPDATE ... SKIP
  LOCKED` (`TestEveryRowLockSkipsLocked`): the generation row in classification,
  and the evidence and candidate rows in the CTE that picks them. A row another
  session holds is left in place, the generation keeps its generation row (it is
  deleted only when no evidence and no candidate row remains), and the settle
  read counts it `lock_busy`. That, not a timeout, is what keeps the batch out of
  a lock cycle with the purge. No session advisory lock, no pass-long
  transaction, no global single flight, no waiting advisory lock.
- The two row-picking deletes take `LIMIT $2` (`BatchRows`) before their lock
  clause, so a batch is bounded in rows as well as in generations: the limit
  counts the rows taken, never the ones SKIP LOCKED skipped
  (`TestPickingDeletesAreBoundedByTheRowLimit`, the large-generation live tests).
  A generation the limit cut is not busy: when a picking delete returned its full
  limit the batch hands the generations the settle read named back to `Sweep`
  (`batchOutcome.continued`), which runs them again before the next slice under
  the same row cap and deadline. Only a batch that was not cut counts what is left
  as `lock_busy`. Do not count a cut generation as skipped, and do not loop
  without the budget check.
- `lock_timeout` is transaction local, 500 ms and never zero (zero disables it).
  It is a backstop for the one wait the batch cannot skip, a relation-level lock
  such as DDL (`TestRelationLockHeldByAnotherSessionEndsTheBatchOnTheLockTimeoutLive`).
  It is not a cycle breaker: a lock timeout only ends the waiter that carries
  it, and the purge's deadlock check can fire first.
- A 55P03 aborts the whole batch: count every generation of it as
  `lock_timeout`. A 40P01 is returned as an error, never absorbed.
- The batch commits what it deleted even when it finished no generation (a
  purge held the rest), so `Result.RowsSwept` can be non-zero while
  `GenerationsSwept` is zero. A batch that deleted nothing and finished nothing
  ends without a commit.
- Deletes are off by default (`Options.Deletes`). A census-only pass opens no
  batch transaction and sends no `DELETE`.
- This slice never names `resolved_relationships`
  (`TestSweepStatementsNeverTouchResolvedRelationships`); its single registered
  deleter is a later slice.
- The advisory lock class literal in the SQL must equal `LockClass`; the
  module-wide scan in `freshness/links/lockclass_scan_test.go` reads the literal.
- `BatchGenerations`, `BatchRows`, `DefaultRowCap` and `CensusListLimit` change
  only with a new measurement in the evidence note. Take it with
  `TestRelationshipRetentionScaleProbe` (`scale_probe_test.go`, opt-in through
  `ESHU_RELATIONSHIP_RETENTION_PROBE`; the note's Setup section has the command).
  Do not add an environment knob here.
- Never import the parent `postgres` package from non-test code.

## Verification

From `go/`:

```bash
go test ./internal/storage/postgres/retention/relationships -count=1
```

The live proofs need a disposable PostgreSQL 18 (`ESHU_POSTGRES_DSN` and
`ESHU_POSTGRES_DISPOSABLE=1`) and run with `-p 1`.
