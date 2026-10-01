# boundederr

Wraps a pgx `driver.Connector` so the `*sql.DB` built from it returns driver
errors that carry only a fixed, bounded message (#7253).

## Why it exists

pgx formats a connection failure as ``failed to connect to `user=<user>
database=<db>`: <host:port>: <dial error>`` and a server error as
`<SEVERITY>: <message> (SQLSTATE <code>)`, where the message can name a relation,
column, or constraint and can echo a bound value. Query handlers write
`err.Error()` into 5xx bodies, so the driver text reached clients. A per-site fix
would touch about 300 handler sites; this package bounds the value once, at
`database/sql`'s driver boundary, below every store that takes a `*sql.DB`.

## Surface

| Symbol | Role |
| --- | --- |
| `NewConnector(inner, opts...)` | wraps a `driver.Connector`; `runtime/postgres` builds the API and MCP server writer pool with it |
| `Wrap(err)` | bounds one error exactly as the pool would, without logging (tests, hand-built errors) |
| `WithLogger(*slog.Logger)` | logger for the operator record (default `slog.Default()`) |
| `Error`, `Kind` | the bounded error; `Kind` is `unavailable`, `timeout`, `canceled`, or `failed` |

`Error.Error()` is one fixed string per `Kind`: `postgres store unavailable`,
`postgres store timed out`, `postgres store request canceled`,
`postgres store statement failed`. `Unwrap` returns the driver's error, so
`errors.Is` and `errors.As` (`pgconn.PgError`, `pgconn.ConnectError`,
`context.DeadlineExceeded`, `driver.ErrBadConn`) classify exactly as before.

## How it works

- `connector.go`: `Connector.Connect` bounds a dial error and wraps the pgx
  connection.
- `conn.go`, `rows.go`: forward every optional driver interface pgx implements
  (`ConnBeginTx`, `ConnPrepareContext`, `ExecerContext`, `QueryerContext`,
  `Pinger`, `NamedValueChecker`, `SessionResetter`, and the four rows
  column-type interfaces) and bound each returned error.
- `error.go`: `classify` picks the `Kind` from the error's type and SQLSTATE,
  never from its text. `passthrough` leaves `io.EOF`, `driver.ErrBadConn`,
  `driver.ErrSkip`, and `driver.ErrRemoveArgument` alone, matched by equality.
- `observe.go`: one `postgres.store.error` log record per failure with the
  driver detail, truncated (see `docs/public/reference/telemetry/logs.md`).

## Limits

- Only pools built from `NewConnector` are bounded. The API and MCP server
  writer pool is (`runtime/postgres`); its reader pool already returns fixed
  texts (#7482). `runtimecfg.OpenPostgres` (ingester, reducer, projector,
  collectors, `admin-status`) is not.
- The wrapped connection is not a `*stdlib.Conn`. Code that calls
  `sql.Conn.Raw` and asserts `*stdlib.Conn` (the bulk `CopyFrom` path in
  `postgres.SQLDB`) must use an unwrapped pool.
