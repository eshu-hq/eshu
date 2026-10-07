# AGENTS.md — reducer/status/summary guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `go/internal/storage/postgres/status/summary/README.md` for the table,
   upsert guard, and advisory lock this runner uses.
3. `go/internal/reducer/AGENTS.md` for reducer-wide rules.
4. `docs/internal/naming.md`: nest plain-English directories, never repeat the
   directory name in a file name, keep this package's tests here.

## Invariants

- One pass is one transaction: READ COMMITTED, jit off, try-lock, clock, statement, upsert,
  commit. Never split the lock and the upsert across transactions: the lock is
  transaction scoped and the single-writer guarantee ends with it.
- A companion model (`Runner.Companions`) joins the same transaction, lock and
  clock. Compute every statement before any upsert. Never open a second
  transaction or a second lock for a model. `validateCompanions` rejects a
  blank key, digest or compute, and a duplicate key.
- Keep the pass at READ COMMITTED (`setReadCommittedSQL`, the first
  statement). Under REPEATABLE READ the guarded upsert raises 40001 when
  another writer committed the row after the pass's snapshot.
- Read `as_of` from the database clock after the lock, never from the host
  clock: replicas' host clocks differ and the upsert guard orders rows by
  `as_of`.
- Never queue or overlap passes. `nextWait` starts the next pass on the first
  interval boundary after the previous one ends. Do not replace the loop with a
  `time.Ticker` (its buffered tick starts a pass back to back after an
  overrun).
- Keep `MinInterval` at 5 s unless new measurements of reducer claim latency
  beside the writer justify a change. A 2 s cadence measurably raised claim
  p95.
- This package holds no status SQL. The statement and its digest come from
  `go/internal/storage/postgres` through `Statement`; only the transaction
  control statements (`setJITOffSQL`, `passClockSQL`) live here.
- A failed pass must never end the loop or crash the reducer. Only an invalid
  configuration makes `Run` return an error.
- Outcome values are a closed metric label set. Add one only with the
  telemetry docs and the dashboards that read it.
- Never import the parent `internal/reducer` package.
