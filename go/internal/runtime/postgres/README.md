# API and MCP PostgreSQL reader access

`Access` owns one writer pool and one private reader pool. The API and MCP
processes will use `Writer()` for authentication, revocation, audit, mutation,
and startup writes. This ordinary pgx writer does not set the infra inventory
`eshu.infra_inventory_writer` derivation marker; API/MCP writes do not all keep
that read model in step. PostgreSQL business reads will receive only `Reader()`'s
`db.Queryer`. Their request boundary must call `ContextWithCheckpoint` after
authorization and before business SQL. A request with no checkpoint fails
before borrowing a reader.

## Configuration

- `ESHU_POSTGRES_DSN` selects the writer. `ESHU_CONTENT_STORE_DSN` is its
  existing API/MCP fallback.
- `ESHU_POSTGRES_READ_DSN` selects a physical streaming reader. When omitted,
  it resolves to the writer DSN at startup. The exact same DSN string in both
  fields also uses a private read-only session pool on the primary.
- `ESHU_POSTGRES_MAX_OPEN_CONNS` (default 30) and
  `ESHU_POSTGRES_MAX_IDLE_CONNS` (default 10) are **totals per API/MCP process**.
  By default, each pool receives 15 open and 5 idle connections. Set
  `ESHU_POSTGRES_READ_MAX_OPEN_CONNS` or
  `ESHU_POSTGRES_READ_MAX_IDLE_CONNS` to change the reader allocation; the
  writer receives the remainder. Both open allocations must be positive.
- Connection lifetime, idle time, and startup ping timeout retain the shared
  runtime PostgreSQL settings. Reader pool acquisition and replay waiting have
  a separate bounded deadline; business SQL uses the caller's request context.

The reader pool sets `default_transaction_read_only=on` on every physical
connection, including reconnects. A DSN's `target_session_attrs=read-write`
selection is replaced on this pool with an actual primary/standby role check:
the setting would otherwise reject the deliberately read-only reader session.
The borrowed connection is checked again immediately before each business
query. Read-only session mode does not replace database permissions; operators
should give distinct readers a read-only database role where practical.

## Freshness and failure

The writer checkpoint reads `pg_current_wal_insert_lsn()`, PostgreSQL system
identifier, database name, and role after the caller's authorization and any
required committed write. A standby query borrows one connection, verifies
read-only session mode, physical recovery role, system identifier, and database
identity, then waits up to the configured bound for replay to reach that
checkpoint. The writer connection has already returned to its pool. A primary
used as both roles passes its role and identity check without replay waiting.
No failed fence falls back to the writer or executes business SQL. The reader
cursor retains its borrowed connection until Close, exhaustion, scan failure,
or request cancellation.

The application database role needs `EXECUTE` on `pg_control_system()` on the
writer and reader; grant that specific function if a hardened role revokes its
public default. No superuser role, automatic grant, or DDL is required.

This contract assumes one static physical replication stream. A scalar LSN and
system identifier do not prove safe lineage after promotion, a timeline fork,
Aurora failover, or a change among multiple reader or writer endpoints. Those
modes require separate topology and consistency proof before enabling them.
The package is not wired into API/MCP yet and does not establish deployed
latency, 100-user capacity, or replica memory requirements.

## Operator signals

`Observer` receives the closed `role`, `stage`, and `outcome` categories with
elapsed time for writer checkpoint, reader pool borrow, identity, replay, and
business query. The API/MCP wiring must attach those to its telemetry provider.
`Stats` exposes both pools' wait and in-use counters for readiness and pool
pressure checks. No DSN, SQL text, or credential becomes a signal label.

## Local proof and limits

Performance Evidence: On the owned PostgreSQL 18.3 physical primary/standby
fixture (primary and reader each 4 CPU/8 GiB, 89 MB test corpus), four
simultaneous fenced queries held four distinct reader connections (`InUse=4`)
and returned all four (`InUse=0`). The focused suite checked one committed
marker visible after replay and zero business SQL on a paused reader; this is
concurrency and exactness proof for the new package. It has no existing
production-path latency baseline because no API/MCP caller uses the package
yet. Per-query fence cost, p95, 100-engineer saturation, and deployed benefit
remain to be measured before application promotion.

Observability Evidence: The production-path test observer saw writer checkpoint,
reader borrow, identity, replay, and business query stages with closed role and
outcome categories. A paused reader emitted replay `deadline` and no business
query event. API/MCP still must wire `Observer` into OTEL; this package does not
claim a deployed signal. The tests reported no reader connections left in use
after Close, exhaustion, scan error, cancellation, or failed identity.
