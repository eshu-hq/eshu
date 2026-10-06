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
upsert, the keyed read, and the writer advisory lock key. It does not own the
statement that fills the row, the writer loop, the reader's fences (schema
version, source digest, age), or any telemetry. The periodic writer (reducer)
and the status reader call this package; neither is part of it.

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

## Proof

Hermetic tests cover the codec round trip and its rejections, the SQL text pins
(strict guard, keyed read), the migration embed and DDL, the error
classification, and the advisory key's uniqueness against every other advisory
key constant in `go/`. Live PostgreSQL 18 tests (`store_live_test.go`,
`bloat_live_test.go`) cover the guard going back never, concurrent writers,
single-row crash safety, the empty and missing-table reads, migration
idempotency, the applied reloptions, and 4,000-upsert bloat (compressible and TOASTed payloads). They run as a blocking step of the reducer contention gate, fail-closed through
`ESHU_REQUIRE_STATUS_SUMMARY_PROOF`, and in the `live-postgres-readiness` lane the
live-test ledger requires. `gate_enrollment_test.go` keeps the workflow step in
step with the tests. The environment names are in the test headers.
