# API and MCP PostgreSQL reader access

`Access` owns one writer pool and either one private legacy reader pool or an
opt-in inventory of private physical-reader pools. The API and MCP
processes use `Writer()` for authentication, revocation, audit, mutation,
and startup writes. This ordinary pgx writer does not set the infra inventory
`eshu.infra_inventory_writer` derivation marker; API/MCP writes do not all keep
that read model in step. PostgreSQL business reads receive only `Reader()`'s
`db.ReadStore`: cursor, row, and read-only snapshot operations. A query-only
optional `db.ReadSnapshotSetBeginner` adds multiple readers on one exported
repeatable-read snapshot; the requested count includes the exporter and cannot
exceed the private pool's connection cap. This optional surface is available
for one physical reader host or an explicit direct-member inventory: each set
uses exactly one member. Native multi-host reader candidates without that
inventory retain guarded cursor, row, and single-connection snapshot reads,
but not snapshot sets: PostgreSQL exported snapshots cannot cross servers.
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
  With direct reader members, the default reader idle allocation rises to
  four per member (8 for two members), within the unchanged total budget.
  An explicit reader idle value below four per member, or a total idle budget
  too small for that floor, fails startup instead of causing repeat dials.
- `ESHU_POSTGRES_EXPECTED_SYSTEM_ID` optionally pins writer bootstrap to an
  externally supplied physical cluster ID. Without it, bootstrap establishes
  agreement among the configured writer and reader endpoints only.
- Native pgx host-list syntax selects candidate hosts within the two pools.
  The reader randomizes host order for each new physical connection. Candidate
  counts never multiply the total open or idle budgets.
- `ESHU_POSTGRES_READ_MEMBERS` optionally names two or more physical standbys
  as JSON objects with `id`, direct `host`, and numeric `port` fields. Each
  `host` may be a DNS name, IPv4 address, or bare IPv6 literal; IPv6 brackets
  and an embedded port are invalid. The inventory has no password or TLS
  material; members inherit the shared read role, database,
  and TLS from `ESHU_POSTGRES_READ_DSN`. Fleet mode requires a distinct,
  single-host read DSN with no pgx fallback (including `sslmode=prefer`'s TLS
  fallback). Member hosts must resolve directly to database Pods/VMs, not a
  Service, load balancer, or proxy. The resolved IP must match PostgreSQL's
  accepted-connection address at bootstrap and on connection validation;
  role, system/database identity, read-only mode, replay, and postmaster
  incarnation are checked separately. Direct-IP matching excludes NAT/proxy
  access and may exclude some dual-stack routes. It is a qualification check,
  not universal physical-identity proof. Restart API/MCP to change inventory.
  Member pools split the existing total read open/idle budgets; each configured
  member needs at least four open and four idle slots for a warm four-way
  code-topic set. A
  member with a recognized transient DNS/transport/SQL availability failure at
  bootstrap remains ineligible until restart. Authentication, completed TLS,
  permission, missing-database, malformed-identity, and unknown setup failures
  abort startup even when another member qualifies. Only a decoded physical
  identity contradiction is classified as wrong topology.
- Connection lifetime, idle time, and startup ping timeout retain the shared
  runtime PostgreSQL settings. Reader pool acquisition and replay waiting have
  a separate bounded deadline; business SQL uses the caller's request context.
  With a direct-member inventory, `Open` caps writer bootstrap and concurrent
  member qualification at one third of its single
  `min(PingTimeout, caller deadline)` budget each; readiness uses the remaining
  deadline. Without an inventory, writer bootstrap and readiness share the
  full startup budget. A successfully qualified member finishes pgx cleanup
  before `Open` can return an `Access`. On a canceled qualification, tracked
  sockets are closed by the stage deadline; `Open` can return while pgx's
  asynchronous cleanup is still pending.

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
per-Access reservation gate protects legacy single-reader sets. In fleet mode,
one allocator atomically reserves all four slots on one member before setup;
it also accounts for single reads, transactions, and readiness checks under
both the member and aggregate caps. An older waiting four-slot set is protected
from later single reads on the same member while another available member can
continue serving work. Cancellation and setup failures release all slots.
Cursor close does not release its transaction. Fleet setup can retry another
member within one overall replay deadline. Each eligible, untried member
receives a share of the remaining deadline after reservation; the last member
receives the remainder. A stalled first member in a two-reader fleet can use
about half the default two-second replay window before failover, so degraded
latency is not guaranteed to be subsecond. A changed local incarnation/address
may fail over only to a different qualified member; writer/checkpoint and
shared configuration mismatches fail closed. No snapshot or partial result
crosses members. The setup attempt bounds transaction start, snapshot export,
and imports. Its timer is detached only after those steps finish; a returned
set remains owned by the caller context until it is closed or canceled.
If an established fleet connection is lost during a snapshot, a narrow
`ReaderMemberLost() bool` error marker permits the caller to retry its whole
read workflow from a fresh snapshot, not an individual SQL statement. The
fresh attempt uses shared round-robin ordering and may select the same member
again; it does not retain a request-specific failed-member exclusion. Auth,
TLS, SQL, caller cancellation, and replay staleness do not carry that marker.
Fleet `Ping` has one ping deadline, captures a writer checkpoint, and requires
at least one reader passing the same frozen identity and replay fence as a
business read. Pool metrics aggregate all reader members. In legacy mode,
guarded borrows and readiness checks own one permit from the reader budget
until their connection is returned. If a legacy snapshot set's permit wait
expires while the request remains live, it releases its partial reservation
and reports a typed capacity error. The
code-topic handler may
then make one single-statement attempt through the same fenced reader and
checkpoint. Dial, identity, replay, snapshot setup, and business-query errors
do not trigger that fallback. The legacy permit wait retains the configured
reader deadline (two seconds by default), so a contended fallback is not a
subsecond-latency claim.

### Local fleet selection evidence

Performance Evidence: In an isolated PostgreSQL 18.6 primary plus two direct
standbys (one system ID, `postgres` database, 7,870,143-byte database before
and after), the same four-connection snapshot-set probe held A's four slots
and selected B with the same writer checkpoint, eight-slot aggregate cap, and
unchanged storage state. Seven interleaved runs of committed baseline
`553fc06c0` and the candidate selection path used a byte-identical probe.
The baseline second-set selection median was 428,692 microseconds (samples:
428670, 428692, 428566, 429468, 428141, 428927, 429412); the candidate median
was 26,525 microseconds (27409, 26788, 25860, 25849, 26525, 26387, 26872).
Every run ended with four connections on each standby, one exported snapshot
per set, and zero business rows read; no mixed-member result was accepted.
The final follow-up edit classified TLS failures as permanent and did not
change successful selection. Raced integration after that edit passed. This
is a local saturated-member selection measurement, not a full endpoint test.
The later [fixed-corpus endpoint runs](../../../../docs/internal/evidence/7033-exact-code-topic-parallel.md#reader-fleet-follow-up-2026-10-03)
measure the legacy and opt-in fleet paths separately. The deployed ops-qa
`<1s` budget is still unmet. The fixture, volumes, and network were removed.

Root-Cause Evidence: In an owned PostgreSQL 18.6 primary plus two physical
standbys, a healthy legacy writer behind a 500 ms connection delay failed
startup with a 1.2 s ping budget because its bootstrap received only one
third of that budget. A stalled fleet `BEGIN` exceeded its 100 ms setup
attempt and waited for the 600 ms caller deadline because `database/sql`
owned the transaction under the caller context. The corrected path passes
those regressions. Socket stalls at `BEGIN`, snapshot export, and import each
time out the first member, release its four reservations, and return a set
from the healthy member that remains usable after the short attempt ends.
These are bounded behavior checks, not interleaved latency medians or an
endpoint speedup claim. The later fixed-corpus endpoint A/B is recorded above;
it does not establish deployed fleet latency.

Observability Evidence: The `business_query` duration/outcome and closed
member-attempt outcome signals show timeout and failover. No SQL, host,
member ID, or credential was added to telemetry labels.

Observability Evidence: Existing closed-cardinality `reader_borrow`,
`reader_identity`, `reader_replay`, and `business_query` stages report duration
and outcome without SQL or member identifiers. The new allocator records its
reservation wait under `reader_borrow`; pool `Stats()` continues to aggregate
member open/in-use/wait counters. The isolated integration tested cancellation,
member replacement/loss, replay lag, and writer-saturated readiness. Fleet
qualification, connection, reservation, waiter, and attempt metrics are new
closed-cardinality operator signals; no new log key was added.
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
An `Observer` that also implements `ContextObserver` receives the request
context through `ObserveContext` instead of `Observe`; `NewObserver`'s observer
does, so its `postgres.reader_access` stage spans are children of the request
span and the stage histogram sample carries the request context (#7545). A
legacy `Observer` keeps receiving `Observe` with no request identity. Stage
spans now follow the request's sampling decision, so their volume tracks
sampled requests. Independently of any observer, `Access` adds each reader
stage to the `db.StageTimings` accumulator on the request context when one is
present; the impact-findings handler uses it to log per-stage seconds.
`Stats` exposes both pools' wait and in-use counters for readiness and pool
pressure checks. Fleet metrics also expose bootstrap qualification, pool
connections, reserved slots, queued requests, and attempt outcomes per
zero-based inventory ordinal. They use no raw member ID, host, DSN, SQL text,
or credential label; scrapes read in-memory pool and allocator state without SQL.

The active recording span in a request trace receives `postgres.reader_query_start` immediately
before each guarded business SQL call. The event includes the actual borrowed
reader's backend PID and TCP peer address, a sequence unique to this `Access`, and
`postgres.role=reader`. Snapshot transactions copy this identity before
`BeginTx` and keep only the scalar values for their subsequent query calls.
Legacy and direct-member fleet snapshot constructors both use that order.
Snapshot export and import are control SQL; they do not emit a query-start event.
An unsupported driver or address emits `postgres.backend.identity=unavailable`;
that event cannot identify a backend. An unsampled request, missing event, or
lost trace is also unqualified for request-to-backend diagnosis. No SQL, args,
DSN, credentials, or PID metric labels are emitted. To check cancellation,
match a retained event to native `pg_stat_activity` with the PID, backend start
time, physical reader instance, and event time. The socket peer can be a
Kubernetes Service address, so it does not identify the backing pod alone.
Confirm that the backend leaves its active query and transaction after
cancellation; the event alone is not
cancellation proof. Stage spans are children of the request span when the
observer implements `ContextObserver` (see above), so a slow request names the
stage that paid for it.

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

No-Regression Evidence: Fleet composition tests verify one business-start
sequence, no event during snapshot export or failed setup, and release of the
member reservation and connection. An owned disposable PostgreSQL 18.6 primary
verified that a fleet-owned transaction's recorded PID matched
`pg_backend_pid()` on the same lease, with a nonempty TCP peer and no retained
connection. That primary-only check does not establish standby behavior,
deployed trace retention, endpoint latency, or cancellation.

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
Fleet tests additionally require `ESHU_READER_TEST_SECOND_READER_DSN` for a
separately slotted physical standby on the same disposable primary. They cover
snapshot affinity/distribution, aggregate capacity, cancellation, wrong-role
rejection, member replacement/loss cleanup, and one-member readiness.
The physical code-topic mid-read loss test also requires
`ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1`,
`ESHU_READER_TEST_TERMINATE_BACKEND=1`, and the exact
`ESHU_READER_TEST_PRIMARY_CONTAINER`,
`ESHU_READER_TEST_FIRST_READER_CONTAINER`, and
`ESHU_READER_TEST_SECOND_READER_CONTAINER` names. Each of the three DSNs must
point to a distinct direct IP of a running Docker container labeled
`eshu.goal=7033-midread`; the test rejects a missing or mismatched fixture
before it creates its proof database or terminates a backend. Never set these
opt-ins for a shared or deployed database.

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
