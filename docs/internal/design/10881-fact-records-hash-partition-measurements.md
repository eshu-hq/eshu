# fact_records HASH(scope_id): measurements on a real sample (#10881)

Refs #10881. Part of #10860. Companion to
[the design record](10881-fact-records-hash-partition-proof.md) and
[the census record](10881-fact-records-census-and-readers.md). Everything here
runs from `scripts/dev/10881-partition/` (its README gives the order).

## Sample, method, and limits

**Source.** A whole-scope sample of `fact_records` from the QA read replica
(`pg_is_in_recovery() = t`; every statement a `SELECT` or `COPY TO STDOUT` in a
session forced to `default_transaction_read_only=on`; the primary was never
touched). The replica holds 94.6M rows in 12,646 scopes: 524 GB in total, 193 GB
heap, 139 GB TOAST, 188 GB across 153 indexes, one of which
(`fact_records_scope_generation_keyset_idx`) is 89 GB.

**Sizes.** Scopes are kept whole. The largest real scope has 12.2M rows (13% of
the table, roughly 37 GB of heap and TOAST), which cannot stay whole under the
60 GB disk cap with two table shapes loaded. Each size keeps the largest scope
that fits whole, plus seeded-random whole scopes (322 repository and small
vulnerability scopes for S1; the same plus the 3.3M-row scope for S2):

| Size | Rows loaded | Largest scope kept whole | Share |
| --- | --- | --- | --- |
| S1 | 2,053,440 | 1,110,435 rows | 54% |
| S2 | 5,361,470 | 3,308,030 rows | 62% |

The 12.2M-row scope itself is NOT_CHECKED. The giant-scope share is higher than in
production (13%), the harsher case for hash skew; production-scale skew is
computed from the real census below. The request asked for about 5M and 15M
rows; the 60 GB cap and the shared disk allowed 2.05M and 5.36M.

**Two shapes, same rows.** Each size is one database holding the undivided table
(`public.fact_records`) and a `HASH(scope_id)` modulus-32 twin (`d.fact_records`)
built from it by the migration backfill below, so the rows are identical by
construction (checked: equal row counts, equal id-set hash, equal payload hash on
a 1% sample). Both carry the **retained index profile**: the primary key, the two
scope-generation indexes and the two retention key indexes
(`generationRetentionKeyIndexesQuery`), which is what stays on the table once
#10880 moves reads off it. The six production triggers are on both. A full
153-index divided copy was started and abandoned (see A1); the undivided
153-index S1 plan is kept as a reference.

**Host.** Stock `postgres:18.6`, 12 GB container, `shared_buffers=3GB`,
autovacuum off (vacuum is run and timed by hand), `fsync=off` after the first
loads (disposable data; WAL volume is unchanged, `full_page_writes` stays on).
The nvme device was shared with other agents the whole run (I/O pressure "some"
40 to 90%, device 87 to 94% busy), so absolute seconds are load-dependent. Two
size pipelines ran at once, so the cluster-wide WAL counters in the migration
stages are upper bounds; the removal arms read per-backend WAL
(`pg_stat_get_backend_wal`) and are exact.

## A1: key, parts, id rule, and the save

- **Key and count.** `HASH(scope_id)`, modulus 32. `PRIMARY KEY (scope_id,
  fact_id)`; every `ON CONFLICT (fact_id)` becomes `ON CONFLICT (scope_id,
  fact_id)` at the five writer sites in the
  [census record](10881-fact-records-census-and-readers.md).
- **Why 32, not 64.** Neither balances rows, because one scope is 13% of the table
  and hash partitioning never splits a scope. `skew_census.sql` applies
  PostgreSQL's own `satisfies_hash_partition` to the real per-scope counts (12,646
  scopes, 94.6M rows):

  | Modulus | Largest part | Share | Smallest part | Ideal | Most scopes in one part |
  | --- | --- | --- | --- | --- | --- |
  | 32 | 14.07M rows | 14.9% | 0.97% | 3.13% | 433 |
  | 64 | 13.09M rows | 13.8% | 0.30% | 1.56% | 230 |

  The giant scope sets the largest part either way (4.8x ideal at 32, 8.8x at 64),
  so 64 buys almost nothing and doubles the index count. Choose 32; the modulus
  cannot change without a full rebuild.
- **Index multiplication is the real cost of the part count.** Restoring the
  production schema as a divided table gives 5,017 index relations at modulus 32
  (153 per part plus parents). An unscoped `SELECT count(*)` on that copy failed
  with `out of shared memory ... increase "max_locks_per_transaction"` at the
  container's settings (`max_connections=50`, default 64 locks), because the
  query locks every index of every part. The copy needed
  `max_locks_per_transaction=2048`. With `max_connections=100` and the default 64
  the lock table holds 6,400 locks, so one unscoped query would take most of
  them. The retained profile is 165 relations (5 on the parent, 160 on parts) and
  does not have the problem. This is the quantitative reason #10880 lands first.
  The 153-index divided load was also 2x slower than undivided on the same
  rows (piece B: 501 s against 231 s, under different host load), and its second
  piece did not finish in the time box; it was dropped. NOT_CHECKED: 153-index
  divided removal and plans.
- **Storage options move.** The production table sets
  `autovacuum_vacuum_scale_factor=0.02` and `autovacuum_vacuum_insert_scale_factor=0.05`
  on the table. A partitioned parent cannot hold them, so the migration sets them
  on all 32 parts.
- **DDL found by restoring the production schema as divided.**
  `ALTER TABLE ONLY ... ADD FOREIGN KEY` is rejected on a partitioned table
  (drop `ONLY`). Every other statement restored unchanged: all 153 indexes and all
  six triggers, two of which are statement triggers with transition tables.

## A5: migration rehearsal (locks, duration, way back, mixed-version writers)

Stages are in `migration_rehearsal.sh`; each prints seconds and WAL, and a sampler
records every relation lock mode held on the fact tables while the stage runs.
Durations are the measured contended host; scaling to 94.6M rows is linear
extrapolation and labelled so.

| Stage | S1 (2.05M rows) | S2 (5.36M rows) | Observed locks |
| --- | --- | --- | --- |
| Expand: `CREATE UNIQUE INDEX CONCURRENTLY (scope_id, fact_id)` on the old table | 64.6 s, index 686 MB | 495.9 s, index 1.22 GB | `ShareUpdateExclusiveLock` on the table; writers continue |
| Build `d.fact_records`, 32 parts, PK, retained indexes, triggers, FKs `NOT VALID` | 2.9 s | 2.8 s | `ShareRowExclusiveLock` on `ingestion_scopes` and `scope_generations` (the FK adds), catalog-brief |
| Backfill `INSERT ... SELECT` | 698.0 s (2,942 rows/s) | 1,535.2 s (3,493 rows/s) | `RowExclusiveLock` on the new table, `AccessShareLock` on the old |
| `VALIDATE CONSTRAINT` x2 | 179.9 s | 452.6 s | `ShareUpdateExclusiveLock` on the new table, `RowShareLock` on the referenced tables |
| Verify: id-set count and hash, 1% payload sample | 182.7 s; counts, hash and 20,747-row sample equal | NOT_CHECKED | `AccessShareLock` |
| Cutover: lock, catch-up of a 50k-row delta, rename swap | 318.9 s total, **318.8 s under `AccessExclusiveLock`** | NOT_CHECKED | `AccessExclusiveLock` on the old table |
| Way back: reverse catch-up of 20k post-cutover rows, rename back | 257.1 s, 257.0 s under `AccessExclusiveLock` | NOT_CHECKED | `AccessExclusiveLock` on both tables |

S2 verify, cutover and way back did not run: a script edit made while the
pipeline was live killed it after the backfill. Their cost is read from S1.

What the numbers say:

- **The catch-up must not run under the lock.** The 318.8 s exclusive hold is
  almost all the catch-up scan (`ingested_at >= ...` has no index) plus 50k rows
  upserted into a 165-index table, not the rename. A catalog-only swap with the
  delta already drained (stage `swap`) is the number that matters; it was written
  but not run in the time box (NOT_CHECKED, expected milliseconds: the same
  rename took 12 ms on an empty copy). The design therefore needs repeated
  indexed catch-up passes outside the lock (an index on the watermark column or a
  change-capture log table filled by a trigger on the old table) and a final
  pass that is empty.
- **Deletes are not caught by an `ingested_at` catch-up.** Retention and reducer
  retirements delete rows on the old table during the multi-hour backfill; those
  rows would survive in the new one. Either pause generation retention and the
  eight `DELETE FROM fact_records` sites during the window, or capture deletes
  too (the change-capture trigger covers both). NOT_CHECKED: a replay of
  concurrent deletes.
- **Backfill duration is hours.** At S2's 3,493 rows/s the 94.6M-row table would
  take about 7.5 hours; `VALIDATE` about 2.2 hours; the expand index about 2.4
  hours, in addition to the retained-profile index builds. These are
  extrapolations from a 5.36M-row copy on a contended disk, not production
  measurements; the production disk and write rate decide.
- **WAL.** Backfill wrote 16.1 GB (S1) and 36.2 GB (S2) of cluster-wide WAL, about
  2.0x the undivided table size; plan WAL archive and replica lag for roughly
  twice the table (production heap plus TOAST is 336 GB).
- **Disk.** The new table and the old coexist until verification: S2 held 17.9 GB
  (old, with retained indexes) plus a similar new copy.

**Mixed-version writers: a real fork, with measurements.** An old writer
(`ON CONFLICT (fact_id)`) against the divided primary key fails with SQLSTATE
`42P10`; so does a new writer (`ON CONFLICT (scope_id, fact_id)`) against the
undivided table without a `(scope_id, fact_id)` unique index (both reproduced on
S1 and in `fact_id_collision_proof.sql`). Two ways through:

1. **Atomic cutover.** Stop every writer (ingester, reducers, all five sites),
   drain the delta, swap, start new binaries. The window is the whole drain plus
   swap; with an unindexed catch-up it was 319 s at S1, and it freezes ingestion
   across every deployment that writes the table. Way back needs the same freeze
   and binaries that rolled back.
2. **Expand, then contract (recommended).** (a) Build the unique index
   `(scope_id, fact_id)` online on the old table (measured above; writers
   unaffected). (b) Ship binaries whose target is `ON CONFLICT (scope_id,
   fact_id)`; with the expand index present they run against the old table
   (measured: accepted) and the old binaries keep running (measured: accepted). (c)
   When a deploy gate proves no old writer remains, backfill, drain and swap.
   After the swap an old writer fails loudly with `42P10`, so (c) is gated on (b)
   being complete. Because the new binaries work on both shapes, the way back is
   a table rename plus reverse drain with no binary rollback. Cost: one more large
   index on the old table (1.22 GB at S2, about 25 GB at production scale by the
   same bytes per row) for the window. Cross-scope same-id rows would raise a PK
   violation on the old table in this window; the producer census says none exist
   today.

## A6: shared-database tenant note

- Hash partitioning gives no tenant isolation: tenants' scopes hash independently
  over the same 32 parts, so every tenant shares every part, every autovacuum
  worker and every index. A tenant is removed by `DELETE`, as today, not by
  dropping a part.
- One tenant's giant scope lands in one part: at production scale the largest of
  32 parts holds 14.9% of the table, the smallest 0.97% (A1). Autovacuum, bloat
  and checkpoint work for that tenant concentrate there.
- Index count scales with parts: x33 at 32 parts for the current 153 indexes
  (5,017 relations measured), with the lock-table consequence in A1. Autovacuum
  worker time, relcache memory and planner time grow with parts, and unscoped
  queries lock every part.
- Isolation by tenant is a different design: it needs a tenant key on
  `fact_records` (none today; tenants reach scopes through `tenant_scope_grants`)
  or separate databases, a #10862 target question and not this table's key.

## A3: the five busiest fact loads, plans on both shapes (S2, 5.36M rows)

"Busiest" rests on two kinds of evidence. Reads are measured on the QA replica's
`pg_stat_statements` (reset 2026-10-20): the top fact statement by total time is the
reducer's `active_facts` scan, 4,550,987 calls, 219,781 s total, 48.3 ms mean, 286
rows a call; the next is a 12,739-call projector probe at 178 ms mean. Writes and
projector loads run on the primary, whose statistics were not read (out of
bounds), so those are ranked by how often the code path runs: once per projection
cycle, once per ingestion batch. `explain_loads.sql` runs the same five on both
shapes; warm, third run, `EXPLAIN (ANALYZE, BUFFERS)`, retained index profile.
Raw plans: `docs/internal/evidence/10881-fact-records-partition/`.

| # | Load | Undivided | Divided (32) | Divided plan |
| --- | --- | --- | --- | --- |
| L1 | Projector page: `(scope_id, generation_id) ORDER BY observed_at, fact_id LIMIT 5000` (`listFactsQuery`) | 4.07 ms | 3.69 ms | one part, keyset index scan; 5,193 against 5,199 buffers |
| L2 | Reducer `active_facts`: `(scope_id, generation_id, fact_kind, is_tombstone=false)` | 0.97 ms | 0.87 ms | one part |
| L3a | Unchanged guard as shipped: join on `fact_id` only (`unchanged_guard.go`) | 8.82 ms | **not finished in 8 minutes, cancelled** | Hash Join over an Append of 32 sequential scans, estimated cost 1,043,234 against 4,373 undivided: every ingestion batch would scan the table |
| L3b | Unchanged guard with `scope_id` added | 6.83 ms | 5.03 ms | one part, primary-key probes |
| L4 | Streaming upsert, 250 changed plus 250 new rows, rolled back | 2,611 ms | 2,507 ms | arbiter is the new `(scope_id, fact_id)` key; time is dominated by the 250 updated rows, same on both |
| L5 | Cross-scope active facts by kind, keyset `ORDER BY fact_id LIMIT 500` | 8.14 ms | 7.58 ms | nested loop probes an Append of 32 parts per active scope (299 loops); cost estimate 259 against 98 |

What this means:

- Scope-bounded loads (L1, L2, L3b, L4) are as fast on the divided table, because
  they prune to one part.
- **L3a is the one that breaks.** `PRIMARY KEY (scope_id, fact_id)` has no usable
  `fact_id`-only access path, so the shipped guard turns into a scan of all 32
  parts. The fix is mandatory and small (add `scope_id`, known from the batch; L3b)
  but it must ship before, or with, the key change. The same applies to every site
  in the fact_id-only table of the census record.
- L5 shows the other cost: a cross-scope load does 32x the index probes per outer
  row. At 299 active scopes it was within noise here, because everything was
  cached; at production scale (12,646 scopes, cold parts) the estimate (2.6x here)
  is the number to watch. NOT_CHECKED at production scale.
- The `scanner_image_result_*` tables the real L2 joins are empty in the
  rehearsal schema; the scope scan is the part under test. The 153-index S1 plan
  (undivided only) is kept in the evidence folder for reference.

## A2: removal, undivided against divided

The shipped retention drain (`generation_retention_chunk_predrain_batch.go`) pages
one superseded generation's fact ids oldest-first through the keyset index and
deletes each page by `fact_id = ANY(...)`, one committed transaction per page.
`removal_procs.sql` reproduces it as a procedure with a `COMMIT` per page (page
100,000 rows, the shipped default). Two arms: `drain_asis` (that SQL, on the
undivided table) and `drain_scoped` (the divided form, `scope_id = $1 AND fact_id =
ANY(...)`). The removal set is the first quarter, by generation id, of the superseded
generations' rows, identical in both shapes; each arm runs in one session
(per-backend WAL via `pg_stat_get_backend_wal`) and is followed by `VACUUM
(VERBOSE, INDEX_CLEANUP on)` in the same session. The superseded rows are real:
the replica's 3.3M-row scope holds 3.0M rows in 11 superseded generations that
retention has not yet drained.

Results at the time-box deadline (host load 24 to 26, I/O pressure 39 to 52%):

| Size | Arm | Delete | WAL (delete) | `VACUUM` | WAL (vacuum) | Rows removed |
| --- | --- | --- | --- | --- | --- | --- |
| S1, 2.05M rows | undivided `drain_asis` | 243.9 s | 0.86 GB | 250.3 s | 5.17 GB | 57 generations (about 25% of superseded rows) |
| S1 | divided `drain_scoped` | running at the deadline | | | | same 57 generations |
| S2, 5.36M rows | undivided `drain_asis` | running at the deadline | | | | 83 generations |
| S2 | divided `drain_scoped` | not started | | | | same 83 generations |

The divided arms and the S2 arms did not finish inside the 3 hour box
(NOT_CHECKED). Until they land there is no divided-against-undivided removal
number, so the issue's kill criterion ("divided materially better on retired-snapshot
DELETE plus VACUUM") is neither met nor failed. What the undivided arm already
shows: after a delete of this size `VACUUM` costs as much as the delete and writes
6x its WAL, because it rewrites index pages; that is the cost a divided table has
to beat, and with the retained five indexes it is already the smaller case (the
production table has 153).

Not a partition drop: `HASH(scope_id)` has no `DETACH`/`DROP PARTITION` path for a
generation or a scope, because every part holds many scopes. Whole-part removal
(`TRUNCATE` of a part) is only valid when every scope in it is retired, which is
a different design (per-scope or per-snapshot partitioning) and is not measured
here.


## NOT_CHECKED

- The 12.2M-row scope, 15M-row sizes, and the 153-index divided shape beyond load.
- S2 verify, cutover and way back; the catalog-only `swap` stage.
- Concurrent writers during backfill (catch-up under load) and delete replay.
- Primary-side `pg_stat_statements` (out of bounds); write-side "busiest" is by
  code path.
- The real-stream collision replay (needs two scopes presenting one id).
