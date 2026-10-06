# Status summary read model: table and store (#7009, PR-A)

## Scope

This note covers the first slice of the #7009 status read model: migration 161
(`status_summary_snapshots`) and the `go/internal/storage/postgres/status/summary`
store. Nothing calls the store yet. The periodic writer (reducer) and the status
reader switch are later slices, so no status route, no reducer loop, and no
query changes in this slice. The design and its gates are in the arbiter ruling
for #7009. This note records only what this slice proves.

The table holds one row per model key. The row's `rows` jsonb is the whole
statement result as `[[section, ordinal, section_json_text], ...]`. The write is
one guarded single-row upsert, so a reader sees the whole old answer or the whole
new one, and an older pass cannot overwrite a newer one.

## Proof

Hermetic tests in `go/internal/storage/postgres/status/summary`:

- codec round trip, the shim tuple shape, empty payload as `[]`, and strict
  rejection of malformed payloads (`ErrDecode`);
- `Row.Validate` and the `row_count` mismatch (`ErrRowCountMismatch`), including
  that `Upsert` never touches the database for an invalid row;
- SQL text pins: strict `existing.as_of < EXCLUDED.as_of` guard, keyed single-row
  read with no filter, ordering or limit, transaction-scoped try-lock;
- migration 161 is embedded, matches the ruled DDL, adds no index beyond the
  primary key and no backfill, and every column the statements name is declared;
- SQLSTATE 42P01 is classified as `ErrNotInstalled` on `Upsert` and `Read` while
  other errors keep their SQLSTATE reachable;
- `WriterLockKey` differs from every advisory key constant in `go/` (an AST scan
  of non-test sources, with a seeded collision showing the scan can fail).

Live tests on PostgreSQL 18.6 (`TestStatusSummary*Live`). They run as a blocking
step of the reducer contention gate (fail-closed through
`ESHU_REQUIRE_STATUS_SUMMARY_PROOF`, kept in step with the workflow by
`TestStatusSummaryProofsRunInTheReducerContentionGate`) and in the
`live-postgres-readiness` lane that the live-test ledger requires:

- missing table: `Read` and `Upsert` return `ErrNotInstalled`;
- migration applied twice without error, the table is empty after it, `Read`
  returns `ErrNotFound`, `pg_class.reloptions` carries `fillfactor=50` and the
  four autovacuum settings, and the table has only its primary key;
- guard: a newer `as_of` advances the row, an older and an equal `as_of` are
  rejected and leave every stored field unchanged. The same sequence run against
  the production statement with its `WHERE` guard cut off moves `as_of` back, so
  the guard is what holds it. Mutating the production guard to `<=` fails the
  equal-`as_of` case, and deleting it fails the guard and the concurrent test;
- two concurrent unlocked guarded writers for 30 rounds, alternating which one
  starts first, beside a polling reader: the final `as_of` is the newer one in
  every round, the reader never saw `as_of` go backwards, and both outcome orders
  occurred (in one run, 14 rounds with only the newer row upserted and 16 with
  both upserted);
- crash safety: a backend terminated after the upsert statement and before commit
  leaves the stored row exactly as it was, and the next pass replaces it;
- writer lock: a second transaction cannot take the lock while the first holds it,
  it can after a rollback, and a terminated holder frees it.

Performance Evidence: no baseline, because the table and store are new and have
no caller. Bloat measured on PostgreSQL 18.6.0 (Homebrew, native, loopback,
default settings) at the shim's production row shape: 35 tuples, a 2,424 byte
payload, 2,000 upserts, then VACUUM, then 2,000 more and VACUUM again. Result: 1
heap page, 0 TOAST pages (the payload compresses inline), 5 dead tuples,
n_tup_upd 3,999 of which 3,999 were HOT (ratio 1.000), and the model read touched
1 shared buffer. With a payload that does not compress (3,186 bytes of random
text) the heap stays at 1 page and every update stays HOT (3,999 of 3,999), the
read touches 1 heap buffer plus 11 TOAST buffers in the serialization step, and
the TOAST relation holds 728 pages after the first round and 789 after the second
(+8 percent). Dead TOAST chunks accumulate until the next VACUUM: 4,000 upserts
finished in about a second, so the proof vacuums by hand and the size reflects the
write volume between vacuums, not the update count. The TOAST relation does not
inherit the table's autovacuum settings, so in production it is vacuumed by
autovacuum's defaults. The production payload is measured at 2.1-2.4 KB and
compressible, so it is expected to stay inline; the TOAST path is measured so a
larger backlog that crosses the threshold is a known cost and not a surprise.
On this fixture the planner reads the one-page table with a sequential scan, not
the primary key index. A later gate must assert buffers touched, not an index scan
node. These figures come from a shared host at load 30 and above, so they are
structural (pages, HOT ratio, buffers), not timings. The deployed latency claim
and the 5 s cadence cost are measured in the later slices.

No-Observability-Change: the package adds no metrics, spans, or logs because no
runtime path calls it yet. The writer slice registers the pass duration, overrun,
age, and writer-up instruments and the reader slice registers the read counter.
The store returns classified errors (`ErrNotInstalled`, `ErrNotFound`,
`ErrDecode`, `ErrRowCountMismatch`) and keeps the SQLSTATE reachable through
`errors.As`, so those callers can label outcomes and log the SQLSTATE.

## Safety

- Additive and reversible: one new table, no change to an existing table, and no
  caller. The migration is idempotent (`CREATE TABLE IF NOT EXISTS`).
- One-way door: the migration ledger pins the file checksum, so any later change
  is a new migration. The storage parameters can be changed with `ALTER TABLE`.
- Concurrency: the writer will hold `pg_try_advisory_xact_lock(WriterLockKey)` on
  the same transaction as the upsert, so one replica writes per tick and a crashed
  holder frees the lock with its backend. The upsert claims no work items and
  uses no `FOR UPDATE SKIP LOCKED`, so queue lease and EvalPlanQual proofs do not
  apply; the only row it touches is its own model row.
- Rolling upgrade: `source_sha256` and `schema_version` let a reader refuse rows
  it did not produce the statement for. The reader slice enforces them.

## NOT_CHECKED

- The pinned `postgres@sha256:54451ecb...` image: the proofs ran on native
  PostgreSQL 18.6 because the Docker host is swept by other sessions.
- Behaviour under real autovacuum timing for the TOAST relation.
- The production payload size and compressibility on ops-qa.
- The model read plan on a table with real statistics (the proof table has one
  row).
