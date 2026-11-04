# #9975: per-table autovacuum triggers for fact_records

Issue #9975 (epic #9209, parent #9805) tracks the keyed winners tick breaching
its 60 s watermark bar. The read-only diagnosis in #9805 found that the probe
spends its time on heap pages full of dead row versions that vacuum has not yet
removed. This note records why migration 276 sets two autovacuum storage
parameters on `fact_records`, what it costs, and how to read the result.

## Classification

- Dead tuple pressure on a high-churn table: `fact_records` holds about 102.3M
  rows, and every update of an impact finding writes a new row version that is
  never HOT (the payload is indexed), so dead versions pile up.
- Wrong trigger scale, not a slow vacuum: autovacuum never started, because
  its default thresholds are 20% of the row count.
- Not a query, index, or lock-ordering defect: the probe's index walk is cheap
  and its predicate is fully index-searchable.

Rejected: a covering or index-only shape for the probe (the planner did not
choose Index Only Scan, 28% of pages are not all-visible, and it would add
write amplification to a table with 144 indexes); an active-generation
prefilter (that test is a join after the heap visit and cannot reduce heap
visits). Per-table cost knobs and analyze knobs are not part of this change:
each is an unmeasured theory on a primary that is already CPU-throttled.

## Performance Evidence:

Source: the #9805 read-only diagnosis on the QA environment primary, taken on
2026-11-04. Nothing below was measured after this change; it is the baseline
the change is meant to move.

| Observation | Value |
|---|---|
| Table size | 20,703,672 heap pages (159 GB), reltuples 102,328,592, 144 indexes (84 GB) |
| Dead rows vs default trigger | n_dead_tup 14,569,747 against a trigger of 20,465,768 (71.2%) |
| Inserts since vacuum vs default insert trigger | 17,400,877 against 19,591,565 (88.8%) |
| Last autovacuum | 2026-10-31 06:37:03Z, almost four days before the read; no manual vacuum ever |
| Table reloptions | none (server defaults, both scale factors 0.2) |
| All-visible pages | 14,913,061 of 20,703,672 (72.0%) |
| Probe cost, 10 real triples, 506 returned rows | 46.0 buffers per returned row; cold pass 13,383 ms, 98.9% of it I/O wait |
| Index vs heap split, 197-row triple | 18 index buffers, 5,552 index entries, 5,452 heap blocks visited, 197 visible rows |

The default triggers are scaled to 102M rows, so at today's rates (14 to 21
dead rows per second, 47 to 80 inserts per second) the table would take 3.3 to
4.8 more days to reach the dead-row trigger. Meanwhile the hot triples show
28 to 71 index entries per live row, so most heap visits find no visible row.

Computed triggers after this change (arithmetic from the same reltuples):

| Trigger | Formula | Value |
|---|---|---|
| Dead rows, scale factor 0.02 | 50 + 0.02 x 102,328,592 | 2,046,622 (about 2.05M) |
| Inserts, insert scale factor 0.05 | 1000 + 0.05 x 102,328,592 x 0.9572 unfrozen | 4,898,641 (about 4.90M) |

Both are already exceeded, so the first vacuum starts within one
`autovacuum_naptime` (60 s) of the migration landing. At today's rates, later
vacuums follow every 1.1 to 1.7 days on dead rows and every 17 to 29 hours on
inserts, each removing about 2M row versions. That keeps dead rows near 2% of
live instead of 12 to 20%.

Expected first vacuum: 30 to 60 minutes of wall clock. This is arithmetic only
and NOT measured. One pass over the 20.7M-page heap and the 144 indexes at the
server's default cost settings (cost limit 200, delay 2 ms) is about 15 to 25
minutes of read credits if pages come from disk, plus about 10 to 20 minutes
for dirtied pages. The run will exceed the 10-minute `log_autovacuum_min_duration`
and so will be logged. Expect the keyed ticks to run slower while it runs, and
do not count that hour against the #9805 closing bar.

Proof still owed after the migration reaches a database, on the same primary and
the same 10 triples: the first vacuum's own log line, `pg_stat_user_tables`
dead and insert counters, the probe again as total buffers per returned row
(hit plus read, which does not depend on cache state), and the keyed tick
durations for the hour after the vacuum completes. Buffers per row near 1 to 2
against 46.0 closes the mechanism. Buffers per row still high after a vacuum
that reports millions of rows "dead but not yet removable" means a second
snapshot holder is capping it.

Local proof for the change itself (disposable PostgreSQL 18.6, migration run
through the shipped runner): `pg_class.reloptions` for `fact_records` is empty
before migration 276 and holds exactly the two parameters after; the relation
file is unchanged (catalog-only, no rewrite); a restart skips it by ledger
receipt and a ledger-less replay leaves the options identical. That is a
behavior proof, not a vacuum-duration claim.

## Observability Evidence:

No new metric or span is added. An operator at 3 AM reads the result from
signals that already exist:

- The autovacuum log line for `fact_records` (pages removed and remaining,
  tuples removed, dead-but-not-yet-removable count, removable cutoff, index
  stats, duration, buffer and WAL usage) appears for any run over 10 minutes.
- `pg_stat_user_tables` for `fact_records`: `n_dead_tup`, `n_ins_since_vacuum`,
  `last_autovacuum`, `autovacuum_count`. A healthy table now shows
  `n_dead_tup` resetting near 2% of live rows instead of climbing past 14M.
- `pg_class.reloptions` for `fact_records` confirms the migration applied.
- The per-phase tick histogram and watermark gauge from #9975, once rolled,
  show whether the keyed ticks move after the first vacuum.
- Replica slot lag and primary CPU throttling, read before and after the first
  vacuum, since WAL volume and CPU rise during the pass.

## Lock and rollout

`ALTER TABLE ... SET (autovacuum_*)` takes SHARE UPDATE EXCLUSIVE. That lock
does not conflict with INSERT, UPDATE, DELETE, or SELECT, and for autovacuum
parameters the table contents are not modified. It waits only behind a running
vacuum or ANALYZE of the same table, and the runner bounds that wait with its
per-statement `lock_timeout` and retry budget. Setting a parameter to its
current value is a no-op, so a replay or an earlier one-off `ALTER` on a
database is harmless. There is no reverse migration in this change; a later
migration would `RESET` both parameters if it were ever needed.

## NOT_CHECKED

- The first vacuum's real duration and its effect on tick durations.
- Whether any other session holds an older snapshot; the first logged vacuum's
  removable cutoff is the test.
- Table ownership of `fact_records` under the runtime role on a deployed
  database; the schema Job applies the migration with its own role.
- The deployed PostgreSQL major version; the lock-level text was read from the
  PostgreSQL 17 `ALTER TABLE` page and the proof ran on 18.6.
