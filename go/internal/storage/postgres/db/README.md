# internal/storage/postgres/db

Shared database contracts for the Postgres storage layer. This is the
dependency leaf that every Postgres store builds on: domain subpackages
depend on these interfaces instead of importing the postgres root package.

## Purpose

`db` holds the seven original interfaces every store uses -- `Rows`, `Queryer`,
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

The original write-capable contracts retain their method sets. The new guarded
read contracts add row and snapshot operations without exposing `Exec`, a raw
transaction, or a raw connection.

- `Rows` -- row cursor (`Next`, `Scan`, `Err`, `Close`).
- `Queryer` -- read-only adapter (`QueryContext`).
- `Executor` -- write adapter (`ExecContext`).
- `ExecQueryer` -- `Queryer` plus `Executor`.
- `Transaction` -- `ExecQueryer` plus `Commit` and `Rollback`.
- `Beginner` -- opens a `Transaction` (`Begin`).
- `ReadOnlyRepeatableReadBeginner` -- opens a read-only repeatable-read
  `Transaction` (`BeginReadOnlyRepeatableRead`).
- `Row` -- scans one result; the owner of the row releases its connection.
- `RowQueryer` -- reads one row with `QueryRowContext`.
- `ReadTransaction` -- cursor and row reads, `Commit`, and `Rollback` only.
  Its snapshot cursor rejects `*sql.RawBytes` before scanning; use `*[]byte`.
  This restriction does not change ordinary `Rows` or legacy SQL adapters.
- `ReadSnapshotBeginner` -- opens a guarded read-only repeatable-read snapshot.
- `ReadSnapshotSet` / `ReadSnapshotSetBeginner` -- open query-only readers
  sharing one exported read-only repeatable-read snapshot; the requested count
  includes the exporter and is bounded by the reader pool's connection cap.
- `ErrSnapshotReservationCapacity` -- marks only an expired guarded-reader
  permit reservation with a live caller and completed partial cleanup. Callers
  may choose a separately fenced single-statement path; setup or query errors
  do not carry this marker.
- `ReadStore` -- combines cursor, row, and snapshot reads.
- `SearchIndexTermCopyUnsupportedError` -- typed error a driver-capability
  check returns; satisfies `UnsupportedSearchIndexTermCopy() bool` for
  `errors.As` callers.
- `ErrReaderStale` / `ErrReaderUnavailable` / `ReaderRetryAfterSeconds` --
  shared guarded-reader failure sentinels and the retry hint the query layer
  sends with the resulting 503 (#7523); `runtime/postgres` re-exports the
  sentinels. Only `ErrReaderStale` and `ErrReaderUnavailable` joined with
  `context.DeadlineExceeded` (a pool-wait, dial, or identity-check timeout inside
  the replay window) map to the 503; any other `ErrReaderUnavailable`
  (authentication or TLS failure, connection refused, permission denied, a
  client disconnect) stays a 500.
- `WithQuerySummary` / `QuerySummaryFromContext` -- bounded read-name context
  plumbing for the `postgres.query` span's `db.query.summary` attribute.

The seven original interfaces keep the exact names, method sets, and
semantics they had in the root package. There are no aliases left behind in root and no forwarding
wrappers here.

The package also holds three shared statement-argument builders --
`CleanIDs`, `IDPlaceholders`, and `IDArgs` -- hoisted byte-identically from
the webhook trigger store under #6693 so the webhook and incident families
share one implementation without one family importing the other. They are
pure stdlib string/args shaping with no SQL text and no I/O.

Two more pieces of `InstrumentedDB` plumbing hoisted here under #6693's
prerequisite-hoists table: `SearchIndexTermCopyUnsupportedError` (with its
`Driver` field), the generic driver-capability error `SQLDB` and
`InstrumentedDB` both return when a driver cannot satisfy the PostgreSQL COPY
protocol; and `WithQuerySummary` / `QuerySummaryFromContext`, the bounded
read-label context plumbing `StatusStore` and `InstrumentedDB` use so a
labeled read is attributable on the `postgres.query` span. Both hoisted
byte-identically from the root `adapters.go` and `status_read_telemetry.go`.

## Dependencies

Only the Go standard library (`context`, `database/sql`, `fmt`, `strings`).
The package performs no I/O and imports no Eshu package -- not even the
postgres root. A `db` import of root (or of any package that imports root)
would recreate the cycle this package exists to prevent.

## Telemetry

None. Contracts carry no instrumentation; recording stays with the stores
and the root bootstrap paths that already own it.

No-Observability-Change: the original hoist of these interfaces added no
metric, span, log field, status field, worker, queue, lease, retry, or durable
write. SQL
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
  text, no migration state, no telemetry import. The `CleanIDs` /
  `IDPlaceholders` / `IDArgs` builders are the one exception: pure
  argument shaping shared by store families. Shared null/blank value
  shaping lives in the sibling `scalars` package, not here.
