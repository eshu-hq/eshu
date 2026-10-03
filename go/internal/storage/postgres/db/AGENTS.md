# AGENTS.md — storage/postgres/db guidance

## Read first

1. `go/internal/storage/postgres/db/doc.go` -- why this leaf exists and what
   deliberately stayed in root (adapters, bootstrap, advisory lock).
2. `go/internal/storage/postgres/db/contracts.go` -- the original seven interfaces.
   Read `read.go` for the additive guarded read interfaces.
3. `go/internal/storage/postgres/db/README.md` -- ownership boundary and
   exported surface.
4. `go/internal/storage/postgres/README.md` -- root pipeline position and
   store inventory.
5. `docs/internal/design/storage-collector-tree.md` -- the #6692 target tree
   and the recorded SQLDB lock-ownership trap.

## Invariants

- This package holds interfaces only: `Rows`, `Queryer`, `Executor`,
  `ExecQueryer`, `Transaction`, `Beginner`, `ReadOnlyRepeatableReadBeginner`,
  plus the additive `Row`, `RowQueryer`, `ReadTransaction`,
  `ReadSnapshotBeginner`, and `ReadStore`. `ReadSnapshotSet` and
  `ReadSnapshotSetBeginner` are optional query-only contracts; they do not
  widen `ReadStore` or expose transaction control. The read transaction has no
  Exec.
  Every name, method set, and semantic matches what the postgres root
  declared before the hoist. The exceptions are the shared
  statement-argument builders `CleanIDs`, `IDPlaceholders`, and `IDArgs`
  (pure stdlib string/args shaping, hoisted byte-identically so the webhook
  and incident store families share one implementation), plus
  `SearchIndexTermCopyUnsupportedError` and `WithQuerySummary` /
  `QuerySummaryFromContext` (InstrumentedDB plumbing, also hoisted
  byte-identically), and the per-request guarded-reader accumulator
  `ReaderStage` / `StageTimings` / `WithStageTimings` / `StageTimingsFrom`
  (`stage_timings.go`, #7545: stdlib `sync/atomic` only; the stage set is closed
  and fixed-size, every figure is a SUM, and nothing may enter it that is SQL
  text, error text, or an identifier). See `go/internal/storage/postgres/db/README.md`.
- `ErrReaderStale`, `ErrReaderUnavailable`, and `ReaderRetryAfterSeconds`
  (`reader_errors.go`) are the shared guarded-reader failure identities and the
  retry hint. They live here so `runtime/postgres` (producer) and
  `query/querycontract` (HTTP mapping) agree on one error identity without
  importing each other (#7523). `ErrReaderUnavailable` is a classification
  marker, not a transience promise: only with `context.DeadlineExceeded` (a
  pool-wait, dial, or identity-check timeout inside the replay window) does the
  query layer answer a retryable 503. Add no other
  error or policy here.
- Standard library only (`context`, `database/sql`, `fmt`, `strings`, `sync/atomic`, `time`). No
  I/O, no SQL text, no migration state, no telemetry, no Eshu import --
  importing the postgres root (directly or transitively) would recreate the
  cycle this leaf exists to prevent.
- `postgres.SQLDB`, `postgres.SQLTx`, and `postgres.SQLQueryer` implement
  these contracts structurally. Do not redeclare concrete adapters here and
  do not add forwarding wrappers or root aliases.
- Store constructors take the narrowest `db` surface they need
  (`db.ExecQueryer`, `db.Queryer`, or `db.Beginner`); `go/cmd/*` keeps
  constructing `postgres.SQLDB`.

## Common changes

- When a Postgres domain moves to a subpackage under #6693, its store
  constructors and fields already reference `db.*`; the move only changes
  the store's own package path, not its database surface.
- When a new store needs a transaction, take `db.Beginner` and call `Begin`;
  for legacy snapshot reads take `db.ReadOnlyRepeatableReadBeginner`; guarded
  reader access takes `db.ReadSnapshotBeginner`. Do not widen
  an existing constructor to `db.ExecQueryer` unless the store writes.
- After touching this package, run `go build ./internal/storage/postgres/...`
  and `go test ./internal/storage/postgres/db/... -count=1` from `go/`, then
  the recursive postgres tree tests.

## Failure modes

- Adding any import beyond the standard library risks a cycle back into the
  postgres root or the collector/reducer packages it serves; `go list`
  dependency edges must keep `db` a leaf.
- Renaming an interface or method breaks every store constructor signature;
  this package renames nothing -- new needs are new interfaces, reviewed on
  #6693.
- Moving `SQLDB` here without the advisory-lock implementation silently
  degrades bootstrap to unlocked DDL (the `schemaBootstrapLocker` assertion
  stops matching). That move needs the lock machinery, the ledger types, and
  the root tests that share its helpers to move together, plus
  lock-contention proof -- it is explicitly out of scope for the hoist.
