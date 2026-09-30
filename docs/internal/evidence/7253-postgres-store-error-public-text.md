# Postgres Store Error Public Text Evidence (#7253)

#7475 bounded the graph-read error text and listed the Postgres half as a
follow-up (`docs/internal/evidence/7253-graph-read-error-public-text.md`). This
note records the fix for the API and MCP server writer pool, the theory proof
that chose its seam, and a classifier the bounded texts would break.

## The leak

pgx v5.9.2 formats a connection failure as ``failed to connect to `user=<user>
database=<db>`: <host:port> (<host>): dial error: ...`` (`pgconn.ConnectError`)
and a server error as `<SEVERITY>: <message> (SQLSTATE <code>)`
(`pgconn.PgError`), whose message can name a relation, column, or constraint and
echo a bound value. Handlers write `err.Error()` into 5xx bodies, about 300
sites (inventory in the #7475 note), so the driver text reached clients.

Observed before any Postgres change, through the real content handler over a
plain pgx pool against a port nothing listens on:

```
{"detail":"get file content: failed to connect to `user=alice database=appdb`: 127.0.0.1:1 (127.0.0.1): dial error: dial tcp 127.0.0.1:1: connect: connection refused","error":"Internal Server Error"}
```

The password was not in the text; the database user, the database, and the
dialed address were.

## What #7482 already covers

#7482 (guarded reader pools) landed while this change was in review. It moved
the API and MCP server onto `runtime/postgres.Access`: a **reader** pool, whose
errors leave the package as `privateError` (a fixed text per failure site, the
driver error behind `Unwrap`), and a **writer** pool built with `stdlib.OpenDB`,
whose errors are the raw pgx errors above. So business reads were already
bounded; the writer pool (authorization, audit, mutation, sign-in, provider
configuration, and any status read that uses it) was not. That pool is what this
change bounds. DSN parse failures are also already fixed text there
(`parsePhysicalEndpoint` returns `invalid PostgreSQL endpoint configuration`).

## Theory proof (throwaway, before any production code)

A scratch test over `database/sql` and `pgx/v5/stdlib`, with no server needed
because a refused dial yields a real `ConnectError`. It was deleted after the
run; the durable tests are listed below.

| Question | Observation |
| --- | --- |
| Does `database/sql` hand the raw driver text to the caller? | Yes. `PingContext` returned the text above; it contained `user=alice`, `database=appdb`, and `127.0.0.1:1`, and did not contain the password. |
| Does a connector that replaces the error value keep the cause reachable? | Yes. With a connector whose `Connect` returned a fixed-text error with `Unwrap`, the text was `postgres store unavailable`, `errors.As` still found `*pgconn.ConnectError`, `errors.Is(err, syscall.ECONNREFUSED)` was true, and an expired context still satisfied `errors.Is(err, context.DeadlineExceeded)`. |
| What must a wrapper forward? | `*stdlib.Conn` implements `ConnBeginTx`, `ConnPrepareContext`, `ExecerContext`, `QueryerContext`, `Pinger`, `NamedValueChecker`, `SessionResetter` (not `Validator`, `Execer`, `Queryer`). `*stdlib.Rows` implements the database-type-name, length, precision-scale, and scan-type column interfaces (not nullable, not `NextResultSet`). |
| Is the `"conn closed"` text match in `freshness/links` needed? | No. That text is pgconn's `connLockError`, which implements `SafeToRetry() bool` returning true; `pgconn.SafeToRetry` finds it through `errors.As`, and the classifier already calls it. The match is left as is: the ingester pool is not bounded by this change, and a bounded error keeps the cause reachable, so cause-based classification still works. |
| Does anything else on the API or MCP path classify a Postgres error by its text? | Yes, one: `contentReferenceIndexUnavailable` (`internal/query`) read `err.Error()` for the missing `content_file_references` table. It is broken on `origin/main` today, independent of this change: #7482's guarded reader returns `PostgreSQL reader query failed`, so a missing table no longer falls back to the content scan and the cross-repo reference search fails instead. `TestContentReferenceIndexUnavailableClassifiesByTypeNotText` is RED against `origin/main`'s classifier for the guarded-reader case and GREEN with the fix, which reads the `PgError` SQLSTATE 42P01 and relation name through the cause. A text search for `err.Error()`, message variables, and SQLSTATE or `does not exist` strings over the query, MCP, status, and `cmd` trees (and an independent one over the 443 internal packages the two servers depend on) found no other; that search is not a proof of absence. |

## Change

`go/internal/storage/postgres/boundederr` wraps the `database/sql` driver
boundary. `NewConnector(inner)` returns a connector whose connections turn every
connection, statement, rows, and transaction error into an `*Error` whose text
is one fixed string per class: `postgres store unavailable`,
`postgres store timed out`, `postgres store request canceled`, or
`postgres store statement failed`. `Unwrap` returns the driver error. The class
comes from the error's type and SQLSTATE, never its text. `io.EOF`,
`driver.ErrBadConn`, `driver.ErrSkip`, and `driver.ErrRemoveArgument` pass
through unchanged, matched by equality (an `errors.Is` match would let a driver
error that merely wraps `io.EOF` keep its connection target; a test pins that).
`Wrap` exposes the same bound for code and tests that build a pool error by hand.

`runtime/postgres` builds the writer pool through it (`openWriterPool`:
`sql.OpenDB(boundederr.NewConnector(stdlib.GetConnector(cfg), WithLogger(...)))`,
which is what `stdlib.OpenDB` does plus the wrapper). `Config.Logger` carries the
process logger from `cmd/api` and `cmd/mcp-server`. No store and no handler
changed, except the classifier above.

After the change a writer-pool call that cannot connect returns an error whose
text is `postgres store unavailable`, and the operator record carries the detail
(pool level, from `TestWriterPoolErrorsCarryNoConnectionTarget`). No test on this
branch drives an HTTP handler over the writer pool: the content route used in
"The leak" now reads through the guarded reader, so on this base its bodies end in
the reader's text (`PostgreSQL reader query failed`), or
`PostgreSQL writer checkpoint failed` when the writer is down, never in
`postgres store ...`. The writer pool serves the identity, session, admin,
recovery, supply-chain, audit, and sign-in handlers. The record:

```
ERROR postgres store call failed event_name=postgres.store.error failure_class=unavailable postgres_store.operation=connect postgres_store.sqlstate="" postgres_store.statement_head="" postgres_store.error="failed to connect to `user=alice database=appdb`: 127.0.0.1:1 (127.0.0.1): dial error: ..."
```

## Proof

- `TestWriterPoolErrorsCarryNoConnectionTarget` (`runtime/postgres`): RED while
  `openWriterPool` returned `stdlib.OpenDB` (the error carried the user, the
  database, and the address, and no record reached the logger), GREEN through
  the wrapper; it also requires `errors.As(ConnectError)` and the record, detail
  included, on the logger passed in. Returning the pool to `stdlib.OpenDB` turns
  it red again.
- `boundederr` tests run a fake pgx-shaped driver through `database/sql`:
  end of a result set and column types survive, a server error mid-result-set is
  bounded and logged exactly once, a unique violation is bounded and logs at
  WARN, a bad connection still retries on a fresh one, a commit error is
  bounded, a canceled request is bounded and not logged, a timeout logs at WARN,
  and a real refused dial is bounded with its `ConnectError` cause reachable.
  A table test drives `Ping`, `Prepare`, `ResetSession`, `CheckNamedValue`,
  `Close`, `BeginTx`, and `Rollback` to failure and requires a bounded error plus
  exactly one record with the right `postgres_store.operation`.
- Seeded mutations, each restored: `bound` returns the error unchanged (six
  `boundederr` tests go red); the sentinel check uses `errors.Is` (one red); the
  `io.EOF` passthrough removed (two red); classify by error text (one red);
  `Ping` or `Close` unbounded or `ResetSession` labelled with the wrong
  operation (the seam test goes red); the writer pool returned to
  `stdlib.OpenDB` (its test goes red).
- Review found two logging defects, both fixed test-first. A mid-result-set
  failure logged twice, because `database/sql` closes the result set after a
  `Next` error and pgx's `Rows.Close` returns the same error again; the fake
  driver's `Close` now behaves like pgx, the test asserts exactly one
  `postgres.store.error` record (it saw two before the fix), and `rows` returns
  the saved bounded error from `Close`. And the pool logged through
  `slog.Default`, which neither binary configures, so the record would have
  missed the process JSON logger; the pool now takes the process logger.
  `CheckNamedValue` logs at ERROR with `operation=query` and no statement: pgx's
  implementation returns nil, so a failure there would be an Eshu-side
  argument-shape fault, not client input.
- `go test -race` on `boundederr` passes. The telemetry frozen key list test
  pins the four new log keys.

## Not covered

- The ingester, reducer, projector, collectors, and `admin-status` open their
  pool through `runtimecfg.OpenPostgres` and still return the driver text on
  their admin endpoints (`internal/status/http.go`, `internal/runtime` metrics).
  That pool also serves the bulk `CopyFrom` path, which asserts `*stdlib.Conn`
  and would not work through the wrapper; bounding it is a separate change.
- Every business request first runs its freshness checkpoint on the writer pool,
  so during a Postgres outage each business request, not only writer routes, logs
  one `postgres.store.error` ERROR (operation `query` or `connect`). The volume is
  the request rate and is not sampled.
- The wiring that passes `Config.Logger` from `cmd/api` and `cmd/mcp-server` is
  two lines and is proven only at the pool, not by a wiring test: `Access.Open`
  needs a live primary to bootstrap. A nil logger falls back to `slog.Default`.
- No live server: a server error (`PgError`) is exercised through a fake driver,
  not a running Postgres. The connect path is the real driver.
- `docs/public/observability/telemetry-coverage.md` has no row for
  `postgres.store.error`: the file is grandfathered over the Markdown cap and may
  not grow. The record is documented in `docs/public/reference/telemetry/logs.md`.
- Handler sites that write a non-driver internal error (JSON encoding, config)
  are unchanged.

## No-Regression Evidence

No-Regression Evidence (#7253): the wrapper adds forwarding calls on every
writer-pool driver call and never builds an error, a log record, or a string on
the success path. `BenchmarkQueryBare` against `BenchmarkQueryBounded` (one
`QueryContext` returning 20 rows through `database/sql` over an in-memory
driver, three runs each): 28 allocs and 648 B per query bare, 29 allocs and
696 B bounded, identical across all three runs, so the added cost is one
allocation (the rows wrapper) and 48 B. The local timings were 862-886 ns bare
and 941-943 ns bounded, about +75 ns per query; they were taken on the contended
shared laptop and are not evidence. Timing on the remote host is NOT_CHECKED
(the host was unreachable when this was written). A real store call is a network
round trip to Postgres, so the wrapper's fixed per-call cost is small next to
it, but that comparison is an inference, not a measurement. The reader pool is
untouched. No SQL, Cypher, index, transaction, queue, or lease path changes.

## Observability Evidence

Observability Evidence (#7253): a 5xx with a bounded body stays diagnosable.
`postgres.store.error` fires once per failure (a close that repeats an earlier
error adds no second record) with `failure_class`, `postgres_store.operation`,
`postgres_store.sqlstate`, a bounded `postgres_store.statement_head` (the
store's SQL with `$N` placeholders, never a value), and `postgres_store.error`
(the driver text, truncated to 1,024 bytes). The response carries none of these,
so an operator matches a client's `postgres store statement failed` to its
record by the time of the failure and reads the statement and SQLSTATE there. A
canceled request logs nothing, and a timeout or a class 22 or 23 error logs at
WARN, so a client cannot raise an ERROR stream. The record is documented in
`docs/public/reference/telemetry/logs.md`.
