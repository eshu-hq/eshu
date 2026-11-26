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
| S1 (first load, migration and plans) | 2,053,440 | 1,110,435 rows | 54% |
| S2 | 5,361,470 | 3,308,030 rows | 62% |

The 12.2M-row scope itself is NOT_CHECKED. The giant-scope share is higher than in
production (13%), the harsher case for hash skew; production-scale skew is
computed from the real census below. The request asked for about 5M and 15M
rows; the 60 GB cap and the shared disk allowed 2.05M and 5.36M.

The removal repair run (A2) reloaded S1 from a fresh pull two hours later, so its S1
has 2,071,167 rows (live ingestion added rows to the same scopes) and its S0
960,732 (raw `r1-row-counts.txt` pruned per `docs/internal/evidence/README.md`).

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

## A5: migration rehearsal (locks, duration, way back, mixed-version writers; not a proof)

**This is a rehearsal of parts of a migration, not a proof of one.** The design is
rejected (see the verdict below), so the migration was not completed or proven:
S2 verify, cutover and way back did not run, and concurrent writers and deletes
were not replayed. Stages are in `migration_rehearsal.sh`; each prints seconds and WAL, and a sampler
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

**The ranking is by code path, not measured.** The replica's
`pg_stat_statements` (reset 2026-10-20) is a cross-check for the read side only: the top fact statement by total time is the
reducer's `active_facts` scan, 4,550,987 calls, 219,781 s total, 48.3 ms mean, 286
rows a call; the next is a 12,739-call projector probe at 178 ms mean. Writes and
projector loads run on the primary, whose statistics were not read (out of
bounds), so those are ranked by how often the code path runs: once per projection
cycle, once per ingestion batch. The plans themselves are measured. `explain_loads.sql` runs the same five on both
shapes; warm, third run, `EXPLAIN (ANALYZE, BUFFERS)`, retained index profile
(raw plans pruned per `docs/internal/evidence/README.md`).

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
  rehearsal schema; the scope scan is the part under test (raw 153-index S1 plan
  pruned per `docs/internal/evidence/README.md`).

## A2: removal, undivided against divided (R1, two sizes)

**Method (R1).** A 2x2: undivided and divided(32) tables, at S1 (2,071,167 rows) and
S0 (S1 without its 1.11M-row giant scope, 960,732 rows). Both shapes carry the
retained five-index profile and the six triggers. One fixed removal list of 202
superseded generations, drawn only from non-giant scopes and written to a file, is
the same in all four cells, and every cell deleted the same 389,603 rows. Each cell
ran alone, in order, after `VACUUM (FREEZE) fact_records` and `CHECKPOINT`, then in
one session: the shipped retention drain (page of 100,000 ids, one committed
transaction per page; fact_id-only SQL on the undivided table, the
`scope_id AND fact_id` form on the divided one), then `VACUUM (VERBOSE)`. WAL is
per-backend (`pg_stat_get_backend_wal`), so other cluster activity does not count.
The decision rests on WAL bytes and page counts, not seconds (raw output pruned
per `docs/internal/evidence/README.md`).

| Metric | S0 undivided | S0 divided | S1 undivided | S1 divided |
| --- | --- | --- | --- | --- |
| rows deleted | 389,603 | 389,603 | 389,603 | 389,603 |
| delete WAL (bytes) | 972,567,605 | 897,923,324 | 905,479,301 | 905,517,223 |
| VACUUM WAL (bytes) | 62,235,389 | 30,642,438 | 216,690,255 | 30,657,547 |
| delete + VACUUM WAL (bytes) | 1,034,802,994 | 928,565,762 | 1,122,169,556 | 936,174,770 |
| index pages in total (VACUUM VERBOSE) | 138,910 | 81,658 | 214,156 | 137,025 |
| index pages newly deleted | 16,454 | 16,744 | 16,517 | 16,690 |
| heap pages scanned | 163,198 | 109,795 | 170,077 | 113,013 |
| buffers: hits / reads / dirtied | 625,955 / 161,203 / 13,965 | 515,274 / 68,259 / 3,697 | 637,301 / 241,352 / 50,198 | 516,576 / 128,977 / 6,822 |
| size before / after (bytes, sum of `pg_partition_tree` leaves for divided) | not captured / not captured | 4,445,921,280 / 4,438,810,624 | 7,545,962,496 / 7,545,880,576 | 7,280,558,080 / 7,273,725,952 |
| host loadavg (1 min) and I/O pressure avg60 at cell start | 25.5 / 39 | 42.3 / 47 | 23.5 / 60 | 20.9 / 58 |

(Delete and VACUUM seconds moved from 16 s to 498 s with host I/O and are not
evidence; raw logs were pruned per `docs/internal/evidence/README.md`.) The S0 undivided size was not captured because the
size query was fixed between cells (a plain table returns no `pg_partition_tree`
leaf, so the first version reported NULL); the other three cells carry it.

**Result against the pre-stated bar.** The 0.67x bar, the "both sizes" rule and the
1.10x growth limit were set by the arbiter before the run; Decision 4 bullet 2 of the
proof record says only "materially better", so the numeric bar is a pre-stated
reading of it, not text from that record.

- Divided over undivided, delete plus VACUUM WAL: **0.897 at S0 and 0.834 at S1.**
  The pre-stated bar for keeping the design alive was at most 0.67 at both sizes. It is not met
  at either: NO-GO stands.
- Delete WAL is the same in both shapes (0.92x at S0, 1.00x at S1): the delete
  writes the same heap and TOAST changes whichever table holds the rows. Only
  VACUUM WAL differs (0.49x and 0.14x), and it is a small part of the total once the
  table has been frozen first (6% to 19% of undivided delete plus VACUUM WAL).
- Undivided delete plus VACUUM WAL for the same removal set grows 1.084x from S0
  to S1 (limit for "does not grow" 1.10x), so removal cost on the undivided table
  also did not grow with a 2.2x larger table here.
- VACUUM VERBOSE reports fewer index pages for the divided table (81,658 against
  138,910 at S0; 137,025 against 214,156 at S1) while index pages newly deleted are
  equal (about 16.5k to 16.7k). Why the totals differ was not investigated.

**What this does not show.**

- The cells are 1.0% (S0, 960,732 rows) and 2.2% (S1, 2,071,167 rows) of the
  production table's 94.6M rows, with a five-index profile; the kill criterion
  quoted in the proof record says "representative scale".
- Undivided VACUUM WAL grew 3.48x (62.2M to 216.7M bytes) for a 2.2x larger table,
  while divided VACUUM WAL stayed flat (30.6M to 30.7M). The ratio improved with
  size (0.897 to 0.834). Total WAL looks flat across sizes only because the delete
  WAL, which is the same in both shapes and noisy, dominates.
- One run per cell, no repeat.
- This is a NO-GO for HASH(scope_id) on removal cost against the pre-stated bar. It
  is not a finding that partitioning cannot help vacuum: at a larger size or a
  repeat the divided VACUUM advantage could grow.

**Limits of the earlier S1 row.** The first measurement (undivided 243.9 s delete,
250.3 s VACUUM, 5.17 GB VACUUM WAL; divided 276.3 s, 510.8 s, 5.24 GB) is kept in
`removal-arms-s1.log` and is superseded by R1 because of two defects: (1) it had no
pre-arm `VACUUM (FREEZE)`, so each VACUUM paid a first vacuum of a freshly loaded
table (5.17 GB of VACUUM WAL against 0.22 GB for the frozen table above); (2) its
divided arm logged size 0 because `pg_total_relation_size` of a partitioned parent is
0, and it did not export rows deleted per arm. It also kept 54% of rows in one scope
and ran one size. Its S2 arms never finished.

Not a partition drop: `HASH(scope_id)` has no `DETACH`/`DROP PARTITION` path for a
generation or a scope, because every part holds many scopes. Whole-part removal
(`TRUNCATE` of a part) is only valid when every scope in it is retired, which is a
different design (a snapshot-keyed layout) and was not measured here.

## Open questions for the snapshot-keyed layout issue

These were prepared as design forks for HASH(scope_id). That design is rejected, so
they are design inputs for #11202 (the snapshot-keyed layout issue), not forks for
#10881 and not decisions. The sub-key skew what-if below (`skew_split.sql`)
belongs to that issue: any layout keyed on scope must say where a 12.9% scope lands.

**Key choice: plain `HASH(scope_id)` mod 32, or a skew-aware key.** One scope holds
12.2M of 94.6M rows (12.9%), so the part that receives it holds at least 12.9% of
the table whatever the modulus (14.9% at 32, 13.8% at 64 on the real placement).
What that costs: removal and vacuum work for that scope cannot be spread over
parts, and a part's autovacuum, bloat and index size are set by it. Reducer
ownership of parts does not follow from the key (claims come from the whole
queue), so skew does not unbalance reducers; it unbalances Postgres maintenance.
`skew_split.sql` evaluates the cheap alternative on the same census, splitting the
giant scope into k sub-keys (for example `(scope_id, hash(fact_id) mod k)` in the
key):

| Giant split into | Largest part, modulus 32 | Largest part, modulus 64 |
| --- | --- | --- |
| 1 (renamed; placement luck) | 16.8% | 14.9% |
| 4 | 7.1% | 5.2% |
| 8, 16, 32 | 7.0% | 5.2% |

From four sub-keys up the floor is set by the next scopes (2.5% to 3.3M-row
scopes), not by the giant. The price: a scope-bounded load of the giant scope
touches k parts instead of one (pruning is lost for it alone), the PK and every
site that binds `scope_id` must also bind the bucket for that scope, and the id
rule needs a bucket function every writer shares. Plain hash keeps one-part
pruning for every scope and accepts the skew. Not measured: the load plans with a
split key (NOT_CHECKED). Options not evaluated: a LIST partition for named giant
scopes, which also makes whole-scope removal a part drop.

**Cutover shape: expand/contract or atomic cutover.** Expand/contract (recommended in A5)
needs a second large unique index for the window (1.22 GB at S2) and a deploy gate
proving no old writer remains; atomic needs a global writer freeze for the whole
drain. Both need a catch-up that does not run under `ACCESS EXCLUSIVE`: my
measured 318.8 s hold (S1) was an unindexed `ingested_at` scan plus 50k upserts.
The alternative is an indexed watermark or a change-capture trigger that also
records deletes, with passes outside the lock until the final one is empty. Its
cost is one index or one trigger on the hot table for the window and was NOT
measured here.

**Reader prerequisite: `scope_id` into the fact_id-only readers.** Per the arbiter's ruling this is *not* done now: the readers stay as they are because no key change is adopted.
`unchanged_guard.go` (`selectUnchangedFactIDs`) is the hot one (L3a). The others
are the fact_id-only table in the census record (about twenty statements across
the query, reducer and storage packages). The change is mechanical where the scope
is known at the call; any site that cannot supply it needs a per-part `fact_id`
index (32 more indexes) or accepts a full scan. This would be a prerequisite of any
key change; it is not done now.

## Verdict

HASH(scope_id) is a **measured NO-GO** for `fact_records`. It did not make removal
of a retired snapshot cheaper by the pre-stated margin: with the same 389,603 rows
removed, divided delete plus VACUUM WAL was 0.897x (S0) and 0.834x (S1) of the
undivided table, against a pre-stated arbiter bar of 0.67x at both sizes, and hash parts hold many
scopes so no part can be dropped or truncated for a snapshot. The 415 GB rebuild is
not pursued. Decision 4, bullet 2 of the proof record is the kill criterion that
applies. Scope-bounded loads do prune to one part and run as fast as undivided
(A3), but that was never the goal of the change.

What this record does **not** claim: the migration is not proven (A5 is a partial
rehearsal of a design that is rejected); the loads' ranking is by code path, not
measured; the reader inventory is a regex worklist, not exhaustive. Per the arbiter's
ruling, no `scope_id` is added to the fact_id-only readers now, and the question of a
snapshot-keyed layout (retired snapshots dropped, not deleted) moves to its own
issue with the R1 method as its measurement standard.

## NOT_CHECKED

- The 12.2M-row scope, 15M-row sizes, and the 153-index divided shape beyond load.
- S2 removal arms (not ordered by the arbiter); S2 verify, cutover and way back; the catalog-only `swap` stage.
- The S0 undivided size before/after (script fix between cells).
- Concurrent writers during backfill (catch-up under load) and delete replay.
- Primary-side `pg_stat_statements` (out of bounds); write-side "busiest" is by
  code path.
- The real-stream collision replay (needs two scopes presenting one id).
