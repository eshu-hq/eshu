# Status summary snapshots

## Purpose

This package stores and reads the reducer-owned status summary read model in
`status_summary_snapshots` (#7009). The status routes' active-work statement
reads tens of thousands of buffers on a deep backlog. The model replaces that
read with one primary-key row lookup: a periodic writer runs the statement and
stores the whole result as one row, and the status reader serves that row
instead of re-running the statement.

## Ownership boundary

The package owns the table's SQL, the row type, the payload codec, the guarded
upsert, the keyed read, the writer advisory lock key, and the reader's decision
about whether a stored row can be served (`Select`: schema version, source
digest, row count, age, and decode), the age advance, the reader setting, the
shared live-fallback call, and the reader's read telemetry. It does not own the
statement that fills the row or the writer loop; the reducer's writer owns
those and the status store owns the live statement and the decoder. The writer
and the status store call this package; neither is part of it.

The package imports only `storage/postgres/db`. It must never import the parent
`postgres` package: the status store will import this package, and importing
back would be a cycle.

## Table

Migration `161_status_summary_snapshots.sql` creates one row per `model_key`:

| column | meaning |
| --- | --- |
| `model_key` | which summary the row holds; primary key (`active_work_summary`) |
| `schema_version` | encoding version of `rows` (`SchemaVersion`, 1) |
| `source_sha256` | sha256 of the statement text the writer ran |
| `as_of` | the clock value the writer bound as the statement's `$1` |
| `computed_at` | database clock when the row was written |
| `pass_duration_ms` | writer compute time for the pass |
| `row_count` | number of tuples in `rows` |
| `rows` | `[[section, ordinal, section_json_text], ...]` in live order |

The table is tuned for one hot row: `fillfactor = 50` keeps updates HOT, and
zero autovacuum scale factors with a threshold of 50 vacuum it after 50 updates.
The migration file is checksum-pinned. Never edit it after it merges; change the
storage parameters with a new `ALTER TABLE ... SET` migration.

## Exported surface

- `Row`, `Entry`: the stored row and one payload tuple.
- `EncodeEntries`, `DecodeEntries`: the payload codec. Decoding is strict and
  returns `ErrDecode` for anything that is not an array of
  `[string, integer, string]` tuples.
- `Upsert(ctx, exec, row) (advanced bool, err)`: one guarded single-row
  statement. The conflict branch applies only when the stored `as_of` is
  strictly older, so an older pass cannot overwrite a newer one and an equal
  `as_of` rewrites nothing. It returns `false` with no error when the guard
  rejects the write.
- `Read(ctx, queryer, key) (Row, error)`: one keyed lookup with the sentinels
  `ErrNotInstalled` (SQLSTATE 42P01), `ErrNotFound`, `ErrDecode`, and
  `ErrRowCountMismatch`.
- `WriterLockKey`, `TryLock(ctx, tx)`: the single-writer-per-tick advisory lock.
  It is transaction scoped, so a crashed holder frees it with its backend.
- `Store`, `Reader`, `Writer`, `NewStore`: small interfaces for the writer and
  reader callers.
- `Select(ctx, queryer, SelectConfig) (Selection, error)`: the reader's
  decision, run on the status snapshot transaction. It reads the database clock
  and the table's existence in one statement with no relation access (so a
  missing table does not abort the snapshot transaction), then the keyed row
  with its payload undecoded. The fences run cheapest first: missing,
  not installed, version (schema version, then statement digest), row count,
  stale, decode, so a row from another statement version is a `version`
  fallback and is never decoded or counted as corrupt. It returns `SourceModel`
  with the entries aged by `now - as_of`, or `SourceLiveFallback` with a typed
  `Reason` (`missing`, `not_installed`, `version`, `row_count`, `stale`,
  `decode`) and the rejected row's `AsOf` and `Age` when a row was read. A
  database error is returned, not turned into a fallback. A row exactly
  `StaleAfter` old is served; older falls back. `Select` reads the row with its
  own scan of `readSQL` (`readRaw`) because `Read` decodes the payload before
  the caller can judge the version columns.
- `AddAge(entries, age)`: advances the age keys the production decoder reads
  as durations (`oldest_outstanding_age_seconds` in `queue` and `backlog`,
  `oldest_blocked_age_seconds` in `blockage`) by the row's age. Zero ages stay
  zero because the statement clamps an empty set to zero.
- `LoadReadConfig`, `ReadConfig`: `ESHU_STATUS_SUMMARY_READ_ENABLED` (default
  off) and `ESHU_STATUS_SUMMARY_STALE_AFTER` (default `33s`, minimum `10s`,
  validated only while the reader is on).
- `ModelReader[T]`, `Hooks[T]`, `Result[T]`: the process-wide reader of one
  model. `Read` runs the hooks the status store supplies (select, decode, live,
  clone, observe) and returns the decoded model with its source, reason, and
  the clock it is true at; the store (`status_read_telemetry.go`) owns only the
  glue. The settings and the `Flight` live in the `ModelReader`.
- `Flight`: shares one in-flight live statement per process among concurrent
  fallbacks; a follower stops waiting when its own context ends, and runs its
  own call when the leader fails. The process builds one reader (and so one
  `Flight`) at startup; a store built per transaction must not own one.
- `Observe`: the read counter, the served-age histogram, the
  `status.active_work.*` span attributes, and the rate-limited fallback Warn.

## Writer contract

The store trusts the `as_of` it is given, so the caller owns two rules. The writer
pass must run at READ COMMITTED: `INSERT ... ON CONFLICT DO UPDATE ... WHERE`
waits for an in-flight conflicting transaction and rechecks the guard on the
newest committed row, but under REPEATABLE READ or SERIALIZABLE the same conflict
raises SQLSTATE 40001. And the writer must bind `as_of` from the database clock
inside the transaction that holds the advisory lock, so every replica shares one
clock domain: a replica with a fast Go clock would otherwise write an `as_of`
ahead of its data, get a correct later pass rejected, and make the reader's
`now - as_of` understate staleness.

## Rollout order

Migration 160 belongs to PR #7645 (`160_activation_obligations.sql`). Merging
this PR after #7645 is a coordination choice, not a runtime requirement: the
migration tracker applies any unapplied file in path order and gaps are allowed.
The real coupling is a textual conflict in `embed_invariant_test.go`,
`migration_checksum_manifest_test.go`, and `schema_order_test.go`, so whichever
PR merges second rebases and re-pins them. Migration 161 only creates a new
table, so no other ordering applies: the migration, then a writer, then a reader
is safe in every order. A reader that finds no table falls back to the live
statement, and a writer that finds no table skips and keeps looping.

## Proof

Hermetic tests cover the codec round trip and its rejections, the SQL text pins
(strict guard, keyed read), the migration embed and DDL, the error
classification, and the advisory key's uniqueness against every other integer
lock or advisory constant in `go/` (an AST scan; keys written as SQL literals or
hashed from data are not covered). The parent package's
`TestActiveWorkSummaryDecodesStoredSummaryRowsUnchanged` round-trips a stored
payload through the production `activeWorkSummary.add`. Live PostgreSQL 18 tests
(`store_live_test.go`, `conflict_live_test.go`, `bloat_live_test.go`) cover the
guard going back never, two writers that genuinely overlap (one waits on the
other's uncommitted row, proved from `pg_stat_activity`), single-row crash
safety, the empty and missing-table reads, migration idempotency, the applied
reloptions, and 4,000-upsert bloat (compressible and TOASTed payloads). They run
as a blocking step of the reducer contention gate, fail-closed through
`ESHU_REQUIRE_STATUS_SUMMARY_PROOF`, and in the `live-postgres-readiness` lane
the live-test ledger requires. `gate_enrollment_test.go` keeps the workflow
step with the tests. The environment names are in the test headers.
