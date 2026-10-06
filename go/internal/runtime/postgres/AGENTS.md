# PostgreSQL reader access agent guidance

Read `README.md`, `config.go`, `physical.go`, `checkpoint.go`, `reader.go`,
`read_store.go`, and the parent
`go/internal/runtime/AGENTS.md` before modifying this package. `Config` must
use the parent's total pool defaults and preserve the collector writer path.
The private reader pool must remain read-only and never escape as `*sql.DB`.
All business SQL must be fenced on its exact borrowed connection. Keep writer
checkpoint acquisition after authorization and release it before waiting on
a reader. Cursor cleanup owns connection release on every terminal path.

`ErrReaderStale` and `ErrReaderUnavailable` must stay the same values as
`db.ErrReaderStale` and `db.ErrReaderUnavailable`: the query layer classifies
reader failures by those shared sentinels and maps them to a retryable 503 with
`Retry-After` (#7523) only for `ErrReaderStale` and for `ErrReaderUnavailable`
joined with `context.DeadlineExceeded`. This package joins `ErrReaderUnavailable`
onto every reader connection, identity-query, and replay-query failure, so a
bare `ErrReaderUnavailable` is NOT retryable and must stay a 500. A new reader
error path that is genuinely transient must carry a deadline (or a new sentinel
the query layer matches); a permanent one must not wrap `context.DeadlineExceeded`.

The current scope allows native host candidates behind one writer pool and one
reader pool. Writer candidates must resolve to the published physical primary
identity in `writerLineage` (`writer_lineage.go`); reader candidates must
resolve to its streaming standbys, or to the same primary when the DSNs are
exactly equal. The writer validator, the same-primary reader validator, and the
checkpoint read that one shared identity; never copy it into a field or a
closure. A same-cluster restart is re-bootstrapped in place: a changed
incarnation is published before `ValidateConnect` returns only when system ID,
database, and insert timeline (`pg_walfile_name`, not
`pg_control_checkpoint()`) match and `pg_current_wal_flush_lsn()` is at or past
the flushed-LSN watermark. Keep the watermark on the flushed LSN: crash
recovery can end below an observed insert LSN. Keep the insert LSN for the
replay fence. `writerLineageSQL` errors during recovery, so it stays out of the
shared `physicalMetadataSQL` that standbys run. A timeline or watermark failure
latches the Access to `ErrWrongTopology` until restart. A dial whose
observation went stale during its round trip returns `errLineageRaced`
(`driver.ErrBadConn`) and never latches. Hold `writerLineage.mu` only for
in-memory work, never across a query. An unchanged incarnation must cost no
extra query. Direct reader members keep their frozen incarnation: a restarted
member stays ineligible until process restart. Promotion, proxies, Aurora, and
split-brain handling need separate proof. `ReadTransaction` owns its borrowed connection
until Commit, Rollback, or cancellation; a cursor only closes its own rows.
`snapshotStatusReader.read` issues `SET LOCAL jit = off` once, after
`BeginReadOnlySnapshot` and before the read phase (#7009). Keep it there: the
shared `BeginReadOnlySnapshot` paths serve other readers, `SET LOCAL` (not
`SET`) is what keeps a pooled connection clean after Commit and Rollback, and
the guarded transaction sends it through `execControl` so it never counts as a
business query. The live containment tests in
`status_reader_jit_containment_test.go` prove all three.
Snapshot sets reserve all requested reader connections behind a per-Access
context-cancelable gate, fence every connection before any transaction, and
retain the exporter through assembly. Failure and cancellation release every
reserved connection and transaction.
Snapshot cursor Scan must reject any `*sql.RawBytes` destination before touching
other destinations, close through its public Close, and preserve Close errors.
Opt-in `ESHU_POSTGRES_READ_MEMBERS` is credential-free and contains only stable
IDs and direct physical standby hosts/ports; use the shared read DSN for auth,
TLS, and database. Never infer snapshot affinity from a Service hostname,
`sslmode`, or pgx fallback count. Freeze each member's role, system/database,
direct-address, and postmaster-incarnation identity at bootstrap; all set
connections must stay on one member. A setup retry releases the entire prior
attempt first. Never retry one business statement after partial rows; mark
only an established fleet member's transport loss for a caller-owned whole
workflow retry. Reader pool limits remain aggregate across members, and
readiness needs the writer and one qualified reader.
Use the owned disposable PostgreSQL fixture for physical replay tests; do not
point write tests at ops-qa. Coordinate fixture use with other agents. Run the
package tests with and without `-race`; classify a new `*_live_test.go` in the
live-test ledger in the same change.

Live candidate, restart, and lineage-swap tests require explicit owned fixture
environment variables documented in README.md. Never hardcode a session host,
port, or container target in committed tests. They are env-gated plain
`*_test.go` files (`restart_test.go`, `crash_restart_test.go`,
`lineage_change_test.go`), outside the live-test ledger like `access_test.go`;
do not add `scheduled` ledger rows. A restart test must prove the same Access
recovers; a promotion or restore test must assert a precondition that isolates
the predicate it claims to exercise, then require `ErrWrongTopology` and the
latch on later dials.

`Observer` stays the legacy, request-less contract and must not change.
`ContextObserver` is the optional extension: `Access.observe` takes the request
`ctx` at every call site and calls `ObserveContext` when the observer implements
it, `Observe` otherwise, never both. Pass the request context (not
`context.Background()`) at any new `observe` call so the stage span parents to
the span active on the request and the `db.StageTimings` accumulator on it is filled; the four
reader stages map to `db.ReaderStage` in `requestReaderStage`, the writer
checkpoint has no slot. A missing accumulator and observer must cost one
`ctx.Value` lookup (`BenchmarkReaderQueryObserve`, #7545 evidence).

The writer pool is built by `openWriterPool` (`writer_pool.go`) through
`boundederr.NewConnector`, so its driver errors carry a fixed text and the detail
goes to `Config.Logger` as `postgres.store.error` (#7253). Keep it that way: the
writer pool serves handlers that write `err.Error()` into 5xx bodies. Do not
assert `*stdlib.Conn` on a writer-pool connection; the wrapped connection is not
one. The reader pool's errors stay `privateError`.
