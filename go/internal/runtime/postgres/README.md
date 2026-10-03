# API and MCP PostgreSQL reader access

`Access` owns one writer pool and one private reader pool. The API and MCP
processes use `Writer()` for authentication, revocation, audit, mutation,
and startup writes. This ordinary pgx writer does not set the infra inventory
`eshu.infra_inventory_writer` derivation marker; API/MCP writes do not all keep
that read model in step. PostgreSQL business reads receive only `Reader()`'s
`db.ReadStore`: cursor, row, and read-only snapshot operations. A query-only
optional `db.ReadSnapshotSetBeginner` adds multiple readers on one exported
repeatable-read snapshot; the requested count includes the exporter and cannot
exceed the private pool's connection cap. This optional surface is available
only for a single physical reader host. Native multi-host reader candidates
retain guarded cursor, row, and single-connection snapshot reads, but not
snapshot sets: PostgreSQL exported snapshots cannot cross server boundaries.
Their request boundary must call
`ContextWithCheckpoint` after authorization and before business SQL. A request with no checkpoint fails
before borrowing a reader.

## Configuration

- `ESHU_POSTGRES_DSN` selects the writer. `ESHU_CONTENT_STORE_DSN` is its
  existing API/MCP fallback.
- `ESHU_POSTGRES_READ_DSN` selects physical streaming reader candidates.
  When omitted, it resolves to the writer DSN at startup. The exact same DSN string in both
  fields also uses a private read-only session pool on the primary.
- `ESHU_POSTGRES_MAX_OPEN_CONNS` (default 30) and
  `ESHU_POSTGRES_MAX_IDLE_CONNS` (default 10) are **totals per API/MCP process**.
  By default, each pool receives 15 open and 5 idle connections. Set
  `ESHU_POSTGRES_READ_MAX_OPEN_CONNS` or
  `ESHU_POSTGRES_READ_MAX_IDLE_CONNS` to change the reader allocation; the
  writer receives the remainder. Both open allocations must be positive.
- `ESHU_POSTGRES_EXPECTED_SYSTEM_ID` optionally pins writer bootstrap to an
  externally supplied physical cluster ID. Without it, bootstrap establishes
  agreement among the configured writer and reader endpoints only.
- Native pgx host-list syntax selects candidate hosts within the two pools.
  The reader randomizes host order for each new physical connection. Candidate
  counts never multiply the total open or idle budgets.
- Connection lifetime, idle time, and startup ping timeout retain the shared
  runtime PostgreSQL settings. Reader pool acquisition and replay waiting have
  a separate bounded deadline; business SQL uses the caller's request context.

The reader pool sets `default_transaction_read_only=on` on every physical
connection, including reconnects. A DSN's `target_session_attrs=read-write`
selection is replaced on this pool with an actual primary/standby role check:
the setting would otherwise reject the deliberately read-only reader session.
The borrowed connection is checked again immediately before each business
query or row scan. A snapshot fences once before `BeginReadOnlySnapshot` and
retains the same read-only repeatable-read connection until Commit, Rollback,
or request cancellation. Snapshot-set setup fences every reserved reader
before beginning any transaction, then imports the export into each worker
before its first query. The exporter stays open through caller assembly. A
per-Access, context-aware reservation gate prevents two sets from holding
partial pool reservations; cancellation and setup failures release all
acquired connections. Cursor close does not release that transaction.
Every guarded reader borrow and the exposed readiness ping also own one permit
from the configured reader pool budget until their connection is returned. If a
snapshot set's internal permit wait expires while the request remains live, it
releases its partial reservation and reports a typed capacity error. The
code-topic handler may
then make one single-statement attempt through the same fenced reader and
checkpoint. Dial, identity, replay, snapshot setup, and business-query errors
do not trigger that fallback. The permit wait retains the configured reader
deadline (two seconds by default), so a contended fallback is not a
subsecond-latency claim.
Snapshot cursors reject `*sql.RawBytes` before scanning and close the cursor;
callers can scan copied bytes with `*[]byte`. Ordinary cursor and legacy SQL
adapter scan contracts remain unchanged.
The semantic extraction status route requests only its semantic status section
inside this same fenced, read-only repeatable-read transaction. Selection does
not bypass replay checks, deadlines, transaction cleanup, or reader telemetry.
Read-only session mode does not replace database permissions; operators
should give distinct readers a read-only database role where practical.

## Writer pool errors

`openWriterPool` builds the writer pool from `boundederr.NewConnector`
(#7253). Every driver error from that pool has one of four fixed texts
(`postgres store unavailable`, `timed out`, `request canceled`,
`statement failed`), the driver error stays behind `Unwrap`, and the detail is
logged once as `postgres.store.error` on `Config.Logger` (`slog.Default` when
nil). The reader pool keeps its own fixed `privateError` texts.

## Freshness and failure

Startup validates a writable primary and its system/database identity, closes
the bootstrap connection, then freezes its postmaster-start microseconds. Every
new writer connection and every checkpoint must match that frozen incarnation.
A writer restart needs an explicit new Access bootstrap. Startup also validates
a reader role and matching cluster/database before returning Access.

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

`ErrReaderStale` and `ErrReaderUnavailable` are the shared
`storage/postgres/db` sentinels (re-exported here), so the query layer maps a
stale replica (`ErrReaderStale`) or a reader whose connection acquisition (pool
wait or dial) or identity check timed out inside the replay window
(`ErrReaderUnavailable` joined with `context.DeadlineExceeded`) to a retryable
HTTP 503 `backend_unavailable` with `Retry-After` without importing this package
(#7523). Other `ErrReaderUnavailable` failures (authentication, connection
refused, permission denied on the identity query, client cancel) are not
retryable and stay a 500 on the reader path (`ErrWrongTopology` and
`ErrMissingCheckpoint` too); `reader_borrow`/`reader_identity`/`reader_replay` with
`outcome="error"` is their operator signal.
`WithCheckpoint` answers the same 503 and `Retry-After` when the checkpoint step
fails (the writer checkpoint query errors or times out, or the writer fails its
topology check: `ErrWrongTopology` at the checkpoint step, the existing #7527
503, unlike the reader path above); a nil checkpoint source answers the same 503
body with no `Retry-After`, because it is a permanent wiring state (#7536). Replica staleness and connection-acquisition timeouts are visible as
`reader_replay` and `reader_borrow` stage durations with `outcome="deadline"`;
no separate counter exists.

A borrowed connection that fails its topology check is removed from the SQL
pool before it can serve another request. Public API and MCP errors name only
the failed reader or writer stage; private wrapped causes retain
`errors.Is`/`errors.As` classification. Cursor and snapshot terminal errors
follow the same rule, including rollback and Close. Existing closed `role`,
`stage`, and `outcome` telemetry identifies failed work without endpoint,
SQL, or driver text in labels.

The application database role needs `EXECUTE` on `pg_control_system()` on the
writer and reader; grant that specific function if a hardened role revokes its
public default. No superuser role, automatic grant, or DDL is required.

This contract assumes one static physical replication stream and redundant
routes to its accepted primary. A scalar LSN and system identifier do not
prove safe lineage after promotion, a timeline fork, Aurora failover, or
split-brain behavior. Those modes require separate qualification before use.
A lagged pooled reader is refused within the replay bound; it is not rerouted.
API/MCP now use this package. Local qualification does not establish deployed
latency, 100-user capacity, or replica memory requirements.

## Operator signals

`Observer` receives the closed `role`, `stage`, and `outcome` categories with
elapsed time for writer checkpoint, reader pool borrow, identity, replay, and
business query. API/MCP attach those to their telemetry provider through `NewObserver`.
`Stats` exposes both pools' wait and in-use counters for readiness and pool
pressure checks. No DSN, SQL text, or credential becomes a signal label.

The active recording span in a request trace receives `postgres.reader_query_start` immediately
before each guarded business SQL call. The event includes the actual borrowed
reader's backend PID and TCP peer address, a sequence unique to this `Access`, and
`postgres.role=reader`. Snapshot transactions copy this identity before
`BeginTx` and keep only the scalar values for their subsequent query calls.
An unsupported driver or address emits `postgres.backend.identity=unavailable`;
that event cannot identify a backend. An unsampled request, missing event, or
lost trace is also unqualified for request-to-backend diagnosis. No SQL, args,
DSN, credentials, or PID metric labels are emitted. To check cancellation,
match a retained event to native `pg_stat_activity` with the PID, backend start
time, physical reader instance, and event time. The socket peer can be a
Kubernetes Service address, so it does not identify the backing pod alone.
Confirm that the backend leaves its active query and transaction after
cancellation; the event alone is not
cancellation proof. The existing stage spans remain standalone diagnostics.

No-Regression Evidence: On an Apple M5 Max, a 200-iteration Go benchmark of a
recording request and synchronous in-memory trace exporter measured 854.4 ns
per baseline request and 16,426 ns with 22 synthetic query-start events per
request. The difference was 15,571.6 ns per request. This isolates trace event
recording/export into memory; it does not measure the live pgx Raw call, network
export, or deployed request latency. A separate native reader lease theory probe
measured the PID getter at 84 ns p95 over four paired 2,000-call rounds with
zero allocations, but that value is not additive proof of deployed cost.
Observability Evidence: Focused tests proved recording request-parent events
with distinct query sequences, an explicit unavailable event for an unsupported
driver, and ordinary and transaction SQL execution after the start event with
unchanged statement and argument values. Deployed trace retention and native
cancellation remain unverified.

## Local proof and limits

The opt-in `TestReaderQueryIdentityMatchesOneNativeLease` takes only
`ESHU_READER_TEST_READER_DSN` and opens one read-only connection to an owned
physical standby. It compares the production Raw getter and retained request
event with `pg_backend_pid()` on the same lease. It does not open a writer,
mutate data, or prove endpoint cancellation; without the DSN it skips. The
operator must bind the DSN to the intended standby and verify its instance
identity outside this test.

The disposable live tests take `ESHU_READER_TEST_WRITER_DSN` and
`ESHU_READER_TEST_READER_DSN`. Candidate tests additionally take complete
`ESHU_READER_TEST_WRITER_CANDIDATES_DSN`,
`ESHU_READER_TEST_READER_CANDIDATES_DSN`, and
`ESHU_READER_TEST_READ_CANDIDATES_DSN` values. The foreign-first test requires
`ESHU_READER_TEST_FOREIGN_WRITER_FIRST_DSN` and an independently supplied
`ESHU_READER_TEST_EXPECTED_SYSTEM_ID`. The controlled restart test runs only
with `ESHU_READER_TEST_RESTART_PRIMARY=1` and an explicit disposable
`ESHU_READER_TEST_PRIMARY_CONTAINER` target. No fixture host, port, or
container name is embedded in the tests.

The API and MCP public-error wire tests additionally take
`ESHU_AUTH_QUALIFIED_DSN` and `ESHU_AUTH_QUALIFIED_READ_DSN`, pointing at one
owned, fully migrated disposable database and its physical standby. Each test
interrupts only its own temporary TCP proxy after a healthy request. The
PostgreSQL containers remain running and unchanged.

Performance Evidence: On the owned PostgreSQL 18.3 physical primary/standby
fixture (primary and reader each 4 CPU/8 GiB, 89 MB test corpus), four
simultaneous fenced queries held four distinct reader connections (`InUse=4`)
and returned all four (`InUse=0`). The focused suite checked one committed
marker visible after replay, zero business SQL on a paused reader, a pinned
repeatable-read snapshot, and an aggregate six-connection cap across two
reader hosts; this is
concurrency and exactness proof for the new package. It has no existing
production-path latency baseline in these primitive tests. The final
[API/MCP status comparison](../../../../docs/internal/evidence/7009-postgres-read-routing.md)
measures snapshot cost; end-to-end checkpoint cost, 100-engineer saturation
and deployed benefit remain NOT_CHECKED.

Observability Evidence: The production-path test observer saw writer checkpoint,
reader borrow, identity, replay, and business query stages with closed role and
outcome categories. A paused reader emitted replay `deadline` and no business
query event. The API/MCP wiring attaches `Observer` and pool metrics to OTEL; local emission
proof does not claim a deployed signal. The tests reported no reader connections left in use
after Close, exhaustion, scan error, cancellation, or failed identity. A
controlled restart of the owned primary made the old Access reject checkpoints;
a fresh Access rebootstrap accepted the new postmaster incarnation.

## Request and admin boundaries

`WithCheckpoint` sits inside application authentication and captures immediately
before selected business dispatch. `RequiresCheckpoint` selects mounted routes,
exempts proven pure/control handlers, and defaults newly mounted routes to a
checkpoint. An exempt route still cannot execute guarded SQL without a
checkpoint. Ask orchestration re-dispatches every inner tool call through the
authenticated boundary; MCP transport-only operations do not borrow readers.

The writer checkpoint has its own bounded acquisition/query deadline, using
`ReplayTimeout`, while the returned context retains the original caller's
deadline for business SQL. A pool wait cannot delay checkpoint acquisition
indefinitely. A failed checkpoint does not dispatch the handler.

`NewTrustedStatusReader` is reserved for the runtime admin surface. It captures
inside each snapshot/readiness method with a five-second ceiling, so independent
background readiness contexts receive their own checkpoint. Readiness probes
both pools and checks the actual fenced status schema. `/healthz` is independent.
The metrics compositor preserves valid independent telemetry when status cannot
be read, exposing snapshot availability without invented database values.
