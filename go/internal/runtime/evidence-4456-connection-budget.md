# Evidence: whole-stack Postgres connection budget (#4456)

Sets the compose Postgres `max_connections` to cover the sum of every runtime's
per-process pool, binds the previously-unbounded API/MCP pools to the shared cap,
and enforces the envelope with a compose invariant test.

## Change

- `max_connections=${ESHU_PG_MAX_CONNECTIONS:-640}` on base + neo4j compose,
  sized for the largest stack sharing this postgres — the remote-e2e stack, which
  extends it and adds the full collector fleet (19 pool-holding services).
- `cmd/api` and `cmd/mcp-server` now call `ConfigurePostgresPool` after
  `sql.Open`, so their pools honor `ESHU_POSTGRES_MAX_OPEN_CONNS` (default 30)
  instead of the `database/sql` unbounded default.
- `TestComposePostgresMaxConnectionsCoversPoolBudget`: asserts
  `max_connections >= (pool-holding services) * 30 + 20 reserved`.

## Proof

Performance Evidence: full-corpus (913-repo) clean-volume local capture on this
branch, backend NornicDB (default) + Postgres 18, all pool-holding services live,
sampling `pg_stat_activity` every 12s for a 15-minute active-bootstrap window.
Baseline (old default `max_connections=100`, unbounded API/MCP pools): the
incoherent envelope — 10 pool-holding services x 30 = 300 potential, plus
unbounded API/MCP — could exceed 100 under a tuned-up or read-burst load and fail
with `FATAL: sorry, too many clients already` (the #4456 diagnostic failure).
After (this change): observed peak 41 total / 13 active connections vs
`max_connections=640`, zero `too many clients` across bootstrap, reducer,
projector, API, and MCP. At default worker counts the stack sits far below even
the old 100 default; the value of the change is the *coherent ceiling* — the
invariant guarantees `max_connections` covers the worst-case pool budget plus an
admin reserve, so raising workers (the scenario that produced the original
failure) can no longer exhaust connections, and adding a pool-holder or lifting
the per-process pool without raising the ceiling fails the build.

No-Observability-Change: no new instruments, spans, metrics, or status fields.
The change is a Postgres server argument, a pool-cap call reusing the existing
`ConfigurePostgresPool` path (same behavior every other runtime already had), and
a compose-shape invariant test. Operators observe pool health through the
existing Postgres `pg_stat_activity` and the runtimes' existing DB telemetry.

Refs #4456, #3586, #3624.
