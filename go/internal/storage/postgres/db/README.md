# internal/storage/postgres/db

Shared database contracts for the Postgres storage layer. This is the
dependency leaf that every Postgres store builds on: domain subpackages
depend on these interfaces instead of importing the postgres root package.

## Purpose

`db` holds the seven interfaces every store uses -- `Rows`, `Queryer`,
`Executor`, `ExecQueryer`, `Transaction`, `Beginner`, and
`ReadOnlyRepeatableReadBeginner`. They moved here from the postgres root
(`db.go`, `status.go`, `schema.go`) under #6693 so later domain moves cannot
create an import cycle between root and its new children.

## Ownership boundary

This package owns the contract shapes only. The concrete adapters (`SQLDB`,
`SQLTx`, `SQLQueryer`), the schema bootstrap and migration ledger, and the
schema-bootstrap advisory lock stay in the root package with the types they
guard. `SQLDB.withSchemaBootstrapLock` satisfies the package-private
`schemaBootstrapLocker` contract asserted by `applyBootstrapDefinitions`, so
moving `SQLDB` without the lock implementation would silently degrade
bootstrap to unlocked DDL; the lock implementation in turn shares unexported
helpers and ledger types with root tests. That colocation is recorded in
`docs/internal/design/storage-collector-tree.md` and is why this package
holds interfaces, not the adapters.

Callers in `go/cmd/*` still construct `postgres.SQLDB` and pass it to store
constructors that now take `db.ExecQueryer`. Go interfaces are structural, so
no wrapper or adapter was needed and no wire contract changed.

## Exported surface

- `Rows` -- row cursor (`Next`, `Scan`, `Err`, `Close`).
- `Queryer` -- read-only adapter (`QueryContext`).
- `Executor` -- write adapter (`ExecContext`).
- `ExecQueryer` -- `Queryer` plus `Executor`.
- `Transaction` -- `ExecQueryer` plus `Commit` and `Rollback`.
- `Beginner` -- opens a `Transaction` (`Begin`).
- `ReadOnlyRepeatableReadBeginner` -- opens a read-only repeatable-read
  `Transaction` (`BeginReadOnlyRepeatableRead`).

Every symbol keeps the exact name, method set, and semantics it had in the
root package. There are no aliases left behind in root and no forwarding
wrappers here.

## Dependencies

Only the Go standard library (`context`, `database/sql`). The package
performs no I/O and imports no Eshu package -- not even the postgres root.
A `db` import of root (or of any package that imports root) would recreate
the cycle this package exists to prevent.

## Telemetry

None. Contracts carry no instrumentation; recording stays with the stores
and the root bootstrap paths that already own it.

No-Observability-Change: hoisting these interfaces adds no metric, span, log
field, status field, worker, queue, lease, retry, or durable write. SQL
text, migration checksums, ledger keys, and lock behavior are untouched.

No-Regression Evidence (#6693): baseline 72addebef vs hoist head, measured
locally with `go build ./...` (exit 0), `go vet ./...` (exit 0), and
recursive `go test` green for `storage/postgres/...` plus every touched
importer package (entrypoints, graphschemacompat, graphowner, query,
admin/store, cli/admin, cmd/*). Input shape: 7 interfaces hoisted with
byte-identical method sets, ~440 importer files requalified, zero SQL-text
diffs, zero migration changes. Backend: local unit tests only — no live
Postgres was reachable, so live query-plan/latency proof is deferred to CI
(the required-gates aggregate is the blocking authority). Why safe: the
change is a mechanical package requalification — no call graph, query, plan,
batching, concurrency, or ordering change is possible in this diff shape;
generated collector entrypoints were regenerated and their gate is green.

## Gotchas / invariants

- A store constructor takes `db.ExecQueryer` (or the narrower `db.Queryer` /
  `db.Beginner` it actually needs), never `postgres.SQLDB`. Depend on the
  narrowest surface the store actually exercises.
- `postgres.SQLDB`, `postgres.SQLTx`, and `postgres.SQLQueryer` implement
  these interfaces structurally; do not redeclare them here.
- Keep this package free of implementation: no connection handling, no SQL
  text, no migration state, no telemetry import.
