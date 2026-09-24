# internal/storage/postgres/fake

A shared, reusable `db.ExecQueryer` test double for
`go/internal/storage/postgres` and every domain package it moves work into.

## Purpose

Postgres domain packages test their stores against a fake database instead of
a live Postgres connection: they stage queued responses, run the store code,
and assert on the recorded SQL and arguments. Before this package existed,
that fake lived only as unexported test-file code
(`fakeExecQueryer`, `go/internal/storage/postgres/work_queue_lifecycle_test.go`),
duplicated per file because Go cannot import one package's test files from
another. `#6693` splits the postgres root into domain subpackages
(`queue/`, `service/`, `facts/`, ...), and 71 existing helper test files
already serve tests destined for two or more of those destinations. `fake` is
ordinary, non-test code so any of them can import it.

## Ownership boundary

This package owns the generic double: recording calls, serving queued
responses and errors, and running caller-supplied `Route` functions. It owns
no Eshu query knowledge. Its predecessor decided how to answer a `QueryContext`
call by matching the postgres root's private SQL constants (deferred-backfill
scans, workflow-coordinator status reads, and others); a shared package
cannot see another package's private constants, and hard-coding every
caller's domain queries here would turn this leaf into a second copy of the
business logic it stands in for. Callers stage that behavior themselves via
`Routes`.

## Exported surface

- `ExecQueryer` -- the fake; satisfies `db.ExecQueryer` and
  `db.ReadOnlyRepeatableReadBeginner`. Construct it as a struct literal and
  stage only the fields the test needs.
- `ExecCall` / `QueryCall` -- one recorded `ExecContext` / `QueryContext`
  call (query text plus positional args).
- `Route` -- `func(query string, args []any) (rows *Rows, handled bool)`,
  tried in order before the FIFO `QueryResponses` queue.
- `Rows` -- an in-memory `db.Rows`; set `FailWith` to make the call that
  serves it fail instead of returning rows.
- `Result` -- an in-memory `sql.Result` reporting one row affected.
- `Transaction` -- the `db.Transaction` `BeginReadOnlyRepeatableRead`
  returns; delegates to the parent `ExecQueryer` so one fixture answers both
  direct and transactional calls.
- `RowAdapter` -- `func(destCount int, row []any) []any`, reshapes a row
  before `Scan` checks it against the destination count. `ExecQueryer.Adapt`
  and `Rows.Adapt` both take one; `ExecQueryer.Adapt` fills in any
  handed-out `Rows` whose own `Adapt` is nil, so a caller can set it once.
- `LegacyQueueRowAdapter` -- a `RowAdapter` reproducing the two column-count
  reshapes moved reducer-queue fixtures need (see its doc comment). Every
  other package's tests leave `Adapt` unset.
- `CheckPlaceholders` -- returns an error unless a query's `$N` placeholders
  are dense and the highest equals the argument count; store tests call it
  on the `Query` and `Args` an `ExecQueryer` recorded.

## Dependencies

`database/sql`, `context`, `errors`, `fmt`, `sync`, `time` (standard library),
plus this repo's `internal/storage/postgres/db` (the contracts it satisfies)
and `internal/storage/postgres/array` (`Rows.Scan`'s `Float64Array`
destination case, matching the array-scan shape stores actually use).

## Telemetry

None. A test double records calls in memory for assertions; it performs no
I/O and needs no operator-facing signal.

No-Observability-Change: this package is new test-support code with no
runtime code path, metric, span, log field, worker, queue, lease, retry, or
durable write of its own.

## Gotchas / invariants

- `ExecQueryer` guards every field with an internal mutex: it is safe for
  concurrent `ExecContext` / `QueryContext` callers (the parallel-batch write
  path in `content_writer_batch.go` needs this).
- `QueryContext` tries `Routes` in call order, first match wins; only once
  every `Route` declines does it fall back to `QueryResponses` (FIFO). An
  unmatched call with an empty queue fails with an error naming the
  unexpected query, so a missing fixture entry surfaces immediately instead
  of silently returning zero rows.
- `Rows.FailWith` fails the `QueryContext` call itself (`QueryContext`
  returns `(nil, FailWith)`); it is never handed back as a `Rows` value, so a
  caller cannot accidentally call `Next`/`Scan` on a value meant to signal
  failure.
- `Rows.Scan` supports exactly the destination types this package's stores
  use: the Go primitives, `[]byte`, `time.Time`,
  `array.Float64Array`/`[]float64`, and the `sql.Null*` wrappers. Add a new
  case here only when a real store needs a new destination type -- this is
  the one place duplicating that switch across every domain package would
  otherwise happen again.
- This package carries no query-string knowledge for any Eshu domain
  (ingestion, semantic, workflow, `fact_records`, ...). That routing stays in
  the caller's own tests via `Routes`.
- `LegacyQueueRowAdapter` is the one exception to "no domain knowledge": it
  exists so moved reducer-queue fixtures do not need a bespoke `Scan`
  reimplementation for two schema-growth column shims. It is opt-in
  (`Adapt`), never applied by default, and every other domain's fixtures are
  unaffected by it.
