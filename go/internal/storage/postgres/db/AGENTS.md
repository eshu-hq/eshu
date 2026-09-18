# AGENTS.md — storage/postgres/db guidance

## Read first

1. `go/internal/storage/postgres/db/doc.go` -- why this leaf exists and what
   deliberately stayed in root (adapters, bootstrap, advisory lock).
2. `go/internal/storage/postgres/db/contracts.go` -- the seven interfaces.
3. `go/internal/storage/postgres/db/README.md` -- ownership boundary and
   exported surface.
4. `go/internal/storage/postgres/README.md` -- root pipeline position and
   store inventory.
5. `docs/internal/design/storage-collector-tree.md` -- the #6692 target tree
   and the recorded SQLDB lock-ownership trap.

## Invariants

- This package holds interfaces only: `Rows`, `Queryer`, `Executor`,
  `ExecQueryer`, `Transaction`, `Beginner`, `ReadOnlyRepeatableReadBeginner`.
  Every name, method set, and semantic matches what the postgres root
  declared before the hoist.
- Standard library only (`context`, `database/sql`). No I/O, no SQL text, no
  migration state, no telemetry, no Eshu import -- importing the postgres
  root (directly or transitively) would recreate the cycle this leaf exists
  to prevent.
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
  for snapshot reads take `db.ReadOnlyRepeatableReadBeginner`. Do not widen
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
