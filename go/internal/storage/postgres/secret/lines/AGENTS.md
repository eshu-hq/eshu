# AGENTS.md - internal/storage/postgres/secret/lines guidance for LLM assistants

## Read first

1. `README.md` in this directory - the lifecycle and the operator checks
2. `session.go` - `DeferredSessionSQL` and `Ready`
3. `state.go` - the epoch state machine SQL
4. `backfill.go` - `Finalize` and its batch transaction
5. `docs/internal/evidence/7125-secret-lines-side-table.md`

## Invariants you must not break

- **Readiness goes off before the first deferred write, durably.**
  `BeginDeferral` is one autocommit statement the caller waits for. A deferred
  write that precedes it can leave findings missing while readers still trust
  the table.
- **The epoch fence is what makes ready trustworthy.** `Finalize` publishes
  ready only `WHERE epoch = $1 AND state = 'building'`. Never publish without
  the epoch predicate.
- **Lock, delete, derive, in that transaction, in primary-key order.** The
  `FOR SHARE` on the batch's files is what keeps a steady-state writer's update
  and this batch from overwriting each other. Removing it produces duplicate-key
  errors and lost rows (proven by mutation in `contention_live_test.go`).
- **The finalizer yields; a writer waits for one batch, not for a cycle.**
  `LockTimeout` bounds the finalizer's own lock waits and must stay below the
  server's `deadlock_timeout`, so a lock cycle ends with the finalizer's
  timeout, not a deadlock abort of a live writer. A writer blocked on a row a
  batch holds `FOR SHARE` still waits until that batch commits (lock, delete,
  and derive of up to `BatchSize` files: hundreds of milliseconds at 500, tunable
  by `BatchSize`). The finalizer never forms a wait cycle.
- **Page by a non-locking key window, lock by key.** A locking `ORDER BY ...
  LIMIT ... FOR SHARE` that waits on a concurrently moved row returns keys out
  of order, so a cursor from its last row can skip files
  (`keymove_live_test.go`). `backfill.go` scans the window without locking, then
  locks exactly those keys.
- **Never `ALTER TABLE ... DISABLE TRIGGER` to defer.** It takes a table-level
  lock and silences every writer. The gate is the per-session setting the
  triggers test in `WHEN`.
- **Only bootstrap-index may run `DeferredSessionSQL`, and never through a
  transaction-mode pooler.** A binary that skips the derivation must hold
  `AcquireBulkLoadLock` across `BeginDeferral` ... `Finalize`, and run
  `BeginDeferral` first and `Finalize` after. Such a pooler does not reset
  session settings between clients, so the setting can stay on a server
  connection later handed to another binary, whose writes then skip derivation
  while the state is `ready`.
- **The bulk-load lock is what makes ready trustworthy for concurrent loads; the
  epoch is not.** Take it before the schema lock (5318,0), never inside it, and
  release it on every path, returning a failed release as a run error. Never
  turn the wait into a blocking `pg_advisory_lock`: both waiters poll a
  try-lock so neither is ever in the wait-for graph.
- **Never import the parent `postgres` package from non-test code.** It is a
  leaf beside it; the live tests import the parent for the schema and the
  content writer.

## Verification

```bash
cd go && go test ./internal/storage/postgres/secret/lines -count=1
ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN=... ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 \
  go test ./internal/storage/postgres/secret/lines -run Live -race -count=1
```
