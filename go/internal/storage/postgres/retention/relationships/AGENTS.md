# AGENTS.md — storage/postgres/retention/relationships

## Read first

1. `README.md` and `doc.go` in this directory.
2. `sql.go`: every statement and the comments that state why it is shaped so.
3. `../../AGENTS.md` for Postgres storage conventions.
4. `../../repository_retirements_purge_sweep.go`: the #10820 purge this sweep's
   lock order must be read against, and
   `docs/internal/evidence/10823-relationship-retention.md` for the measured
   plans, WAL figures and mutant results.

## Invariants

- Rule A is the only retention key: no `scope_generations` row, not any scope's
  `active_generation_id`, no `active` generation row. Do not widen it.
- Every delete (evidence, candidates, generation row) re-asserts all three
  predicates in its own `WHERE`. `= ANY($1)` only narrows. Never replace the
  predicates with trust in the id list
  (`TestDeleteStatementsReassertEveryPredicate`, and the live adversarial-array
  test).
- The deletes take the classification statement's claimed ids, never the batch
  input: an id whose generation row another session holds must not be deleted
  (`TestDeletesReceiveOnlyTheClassifiedIDs`).
- Delete order is evidence, candidates, generation row last, one transaction.
  The #10820 purge deletes in the same order. Do not reorder.
- Locks: transaction-scoped per-generation try locks and
  `FOR UPDATE ... SKIP LOCKED` on the generation row. No session advisory lock,
  no pass-long transaction, no global single flight, no waiting advisory lock.
- `lock_timeout` is transaction local, 500 ms, below the 1 s
  `deadlock_timeout`, and never zero (zero disables it). It is what keeps the
  purge from ever seeing a 40P01.
- A 55P03 aborts the whole batch: count every generation of it as
  `lock_timeout`. A 40P01 is returned as an error, never absorbed.
- Deletes are off by default (`Options.Deletes`). A census-only pass opens no
  batch transaction and sends no `DELETE`.
- This slice never names `resolved_relationships`
  (`TestSweepStatementsNeverTouchResolvedRelationships`); its single registered
  deleter is a later slice.
- The advisory lock class literal in the SQL must equal `LockClass`; the
  module-wide scan in `freshness/links/lockclass_scan_test.go` reads the literal.
- `BatchGenerations`, `DefaultRowCap` and `CensusListLimit` change only with a
  new measurement in the evidence note. Take it with
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
