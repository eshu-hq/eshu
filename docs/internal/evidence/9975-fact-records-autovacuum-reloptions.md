# #9975: per-table autovacuum triggers for fact_records

Issue #9975 (epic #9209, parent #9805) tracks the keyed winners tick breaching
its 60 s watermark bar. The read-only diagnosis in #9805 found that the probe
spends its time on index entries that point at heap locations with no visible
row. Dead row versions that vacuum has not yet removed are the leading
explanation; the diagnosis left open whether those entries are dead versions,
vacuumed-but-uncleaned line pointers, or something else. This note records why
migration 278 sets two autovacuum storage parameters on `fact_records`, what it
costs, how to read the result, and how to run the first vacuum safely.

## Classification

- Dead tuple pressure on a high-churn table (the reading, not an established
  fact): `fact_records` holds about 102.3M rows, and every update of an impact
  finding writes a new row version that is never HOT (the payload is indexed).
- Wrong trigger scale, not a slow vacuum: autovacuum started rarely (the last
  run was 2026-10-31), because its default thresholds are 20% of the row count.
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
2026-11-04. Nothing in the baseline tables below was measured after this
change; they are the baseline the change is meant to move. The first vacuum
section near the end records a vacuum that ran under the server defaults,
before migration 278 had rolled.

| Observation | Value |
|---|---|
| Table size | 20,703,672 heap pages (159 GB), reltuples 102,328,592, 144 indexes (84 GB) |
| Dead rows vs default trigger | n_dead_tup 14,569,747 against a trigger of 20,465,768 (71.2%) |
| Inserts since vacuum vs default insert trigger | 17,400,877 against 19,591,565 (88.8%) |
| Last autovacuum | 2026-10-31 06:37:03Z, almost four days before the read; vacuum_count 0 (no manual vacuum since the last stats reset) |
| Table reloptions | none (server defaults, both scale factors 0.2) |
| All-visible pages | 14,913,061 of 20,703,672 (72.0%) |
| Index vs heap split, 197-row triple | 18 index buffers, 5,552 index entries, 5,452 heap blocks visited, 197 visible rows |

Probe baselines, the same 10 real triples and 506 returned rows each time, in
total buffers per returned row (hit plus read; this does not depend on cache
state, unlike milliseconds):

| State | Pass | Buffers per row | Disk reads | Exec ms |
|---|---|---|---|---|
| An old snapshot holder open (diagnosis section 7) | 1 (cold) | 46.0 | 23,172 | 13,383 |
| Holder open | 2 (repeat) | 35.4 | 16,377 | 154 |
| After the holder was cancelled (diagnosis section 11) | 1 (cold) | 35.8 | 17,918 | 9,931 |
| After the holder was cancelled | 2 (repeat) | 12.2 | 4,970 | 66 |

The 46.0 figure was taken with the holder open, and the cancel plus kill hints
set by earlier passes had already moved it before any vacuum ran. The
baseline for the vacuum is therefore the after-cancel pair. Buffers per row
are cache-independent on both passes; the repeat pass (12.2) is the
hint-settled comparison, because pass 1 sets the kill hints that pass 2 then
benefits from. This is one before/after pair per arm with no control run, so
the cancel cannot be separated from hint accumulation across passes. Two of
the three 2-row triples (2 and 6) and the 197-row triple (7) moved little
after the cancel (triple 7 went from 5,485 to 4,220 buffers, about 23%);
triple 8, the third 2-row triple, fell from 199 to 7. The 197-row one is
unexplained. These figures predate the first vacuum; the first vacuum section
below records what it did.

The default triggers are scaled to 102M rows, so at the baseline rates (14 to 21
dead rows per second, 47 to 80 inserts per second) the table would take 3.3 to
4.8 more days to reach the dead-row trigger. Meanwhile the hot triples show
28 to 71 index entries per live row, so most heap visits find no visible row.

The insert trigger was much closer, and it mattered for rollout. Arithmetic
made before the run: diagnosis section 11.2 read `n_ins_since_vacuum` at
17,632,252 at 04:21:55Z on 2026-11-04. The default insert trigger is
19,591,565, so 1,959,313 inserts remained. At 80 and at 47 inserts per second
that is 6.8 to 11.6 hours, which is about 11:00Z to 16:00Z on the same day. The
vacuum started earlier, at 09:00:52.7Z (see the first vacuum section). Reaching
the trigger in 4 h 39 min took an average of at least 117 inserts per second,
above the 80.6 maximum the window used (arithmetic). The rates varied (0.5 to
80.6 in the diagnosis windows, 19.7 in the hour after the vacuum), so the window
was indicative only. The consequence held: with no migration and no one-off, a
default-settings autovacuum of `fact_records` started on its own when that
counter crossed its default trigger. That first vacuum measures the theory with
default settings, not migration 278, and a later roll's ALTER would cancel one
that is still running (the rollout-order consequence below applies equally).
Read `last_autovacuum`, `autovacuum_count`, and the roll-time lock check below
before any roll.

Computed triggers after this change (arithmetic from the same reltuples):

| Trigger | Formula | Value |
|---|---|---|
| Dead rows, scale factor 0.02 | 50 + 0.02 x 102,328,592 | 2,046,622 (about 2.05M) |
| Inserts, insert scale factor 0.05 | 1000 + 0.05 x 102,328,592 x 0.9572 unfrozen | 4,898,641 (about 4.90M) |

The insert formula that scales by the unfrozen fraction (`relallfrozen`) is
PostgreSQL 18 behavior. On PostgreSQL 13 to 17 the same arithmetic gives
1000 + 0.05 x 102,328,592 = 5,117,430, still exceeded.

Both were exceeded at the last read, but a first vacuum starts within one
`autovacuum_naptime` (60 s) of the migration landing only where the counters
still exceed the new thresholds at apply time. On QA the default-settings
vacuum has now run, from 09:00:52.7Z to 10:43:24.607Z on 2026-11-04. Its end
reset the counters (`n_dead_tup` 13,333 and `n_ins_since_vacuum` 14), so a roll
that applies 278 afterwards starts no immediate vacuum. The next 278-triggered
vacuum is set by the rates after the first one. In the one hour after it ended,
`n_dead_tup` rose by 189,520 (52.1 per second) and `n_ins_since_vacuum` by
71,824 (19.7 per second). Against the 278 triggers computed above (2,046,622
dead rows, 4,898,641 inserts, from the pre-vacuum `reltuples`), arithmetic from
that one hour puts the dead-row trigger at 10.9 hours (2,046,622 / 52.1) and the
insert trigger at 69 hours (4,898,641 / 19.7). The dead-row trigger is first.
The vacuum changed `reltuples` and `relallfrozen`: the measured post-vacuum
default triggers (dead about 19.29M, insert about 15.97M) imply a `reltuples`
near 96M, which would lower the 278 dead-row trigger by about 6% and the insert
trigger by about 19% (arithmetic), the same order. These rates differ from the
baseline ones (14 to 21 dead and 47 to 80 inserts per second), so read 10.9
hours as one hour's reading, not a steady cycle.

"Dead rows near 2% of live" is the table-wide average, not what the probe
pays. The probe reads hot rows, which carry 28 to 71 index entries per live
row against a table-wide dead share of about 14%. Those entries built up over
about 94 hours, one version per 1.3 to 3.3 hours; the live-row xmin ages in
diagnosis section 9.2 say 0.4 to 1.9 hours. A 10.9 hour cycle therefore
rebuilds about 3 to 8 dead entries per live row on a hot row before the next
vacuum (10.9 / 3.3 to 10.9 / 1.3), or about 6 to 27 using the xmin ages
(10.9 / 1.9 to 10.9 / 0.4). This is arithmetic from the one-hour rate, and kill
hints should lower it by an amount nobody has measured. It means a sawtooth:
near 1 to 2 buffers per row just after a vacuum (measured: 1.4 right after the
first vacuum and 1.3 an hour later), climbing back toward a share of today's
figure before the next one. The hour readback after the first vacuum reads the
bottom of that sawtooth, so it does not show that the 60 s bar holds a day
later (see the extra read below).

First-vacuum duration: measured at 6,151.92 s (1 h 42 min 32 s). The arithmetic
made before the run is kept below to show how far it was off; it was never a
prediction, only a range:

- Credit-bound, the full cost limit to one worker (200 credits per 2 ms cycle,
  so 100,000 credits per second; miss 2, hit 1, dirty 20). Reading the 20.7M
  heap pages plus 84 GB of indexes (about 10.25M pages) as all misses is 10.3
  minutes. A normal vacuum skips all-visible pages, and 72.0% of heap pages
  are all-visible, so heap reads drop to about 5.8M pages and the same
  arithmetic gives 5.3 minutes. Every index is read in full either way.
- Dirtied pages add 20 credits each (about 3.3 minutes per million pages).
  How many pages get dirtied is unknown.
- The cost limit is shared. Autovacuum divides it among the running workers,
  and diagnosis section 9.1 saw two other vacuums running at both reads. With
  two or three workers sharing, 5.3 to 10.3 minutes become 10.7 to 31 minutes.
- Credits are not the only bound. The diagnosis measured 0.571 ms per cold
  read. At that latency, with no readahead, 16.0M to 31.0M reads take 2.5 to
  4.9 hours. That is an upper bound from a random-read figure; sequential
  reads should be faster. It still shows a multi-hour run is possible on this
  CPU-throttled primary.

Result against that arithmetic. The credit-bound floors (5.3 to 31 minutes)
were far under the measured run, and the read-latency bound (2.5 to 4.9 hours)
brackets it. The pass read 23,174,029 buffers (I/O read time 719.4 s) and
dirtied 11,770,455 pages. At 20 credits each that is about 235 million credits,
or 2,354 s at the full unshared limit of 100,000 credits per second (arithmetic
from the credit model above), roughly 38% of the run. I/O read, I/O write
(60.6 s) and CPU (505.8 s user, 156.3 s system) add to 1,442 s of the 6,152 s.
The rest is not accounted for by these figures; waiting on the shared cost
limit is the likely cause (inference, not measured).

The run exceeded the 10-minute `log_autovacuum_min_duration` and was logged
(the 5.3-minute credit floor would not have been). The keyed ticks did not slow:
p95 was 0.945 s over 203 ticks during the run (the audit path is a separate
series, p95 about 228 s). Exclude the whole run of any vacuum from the #9805
closing bar, from the first granted `ShareUpdateExclusiveLock` row on
`fact_records` held by a backend that is not your own session (the roll-time
check below) until `last_autovacuum` updates, whatever its length.

Proof taken after the first vacuum, on the same primary and the same 10
triples (diagnosis section 12; the readings are in the first vacuum section).
It is read against the after-cancel baseline (35.8 cold, 12.2 repeat), not
against 46.0, and against a control taken before the vacuum (7.7 repeat, 13.1
cold at 05:54Z). The decision rule was:

- Buffers per row near 1 to 2 on the repeat pass, and the hour inside the bar:
  the vacuum moved it past what the cancel and hints did, and the mechanism is
  closed.
- Buffers per row still high after a vacuum whose log line reports millions of
  rows "dead but not yet removable": a second snapshot holder caps it. Bring
  the removable cutoff from the log line to the arbiter.
- Buffers per row low but ticks still over 60 s: the tail has another
  component, most likely the write half, which was never measured. The
  per-phase tick histogram (merged in #9981) would name it.

The first bullet applies on these readings (n = 1): 1.4 buffers per row on the
repeat pass right after the vacuum and 1.3 an hour later, the log shows no
second holder (8,736 dead but not yet removable), and the hour after completion
was inside the bar. The tick bar cannot tell the vacuum from the earlier
holder cancel, because keyed p95 was already under 15 s before it; the probe
is what separates them. The per-phase histogram was not read.

One more read, shortly before the second vacuum starts: the same 10-triple
probe plus the keyed tick gauge. It is a single read, not a soak, and it is the
point on the sawtooth that matters. Once migration 278 has rolled, the second
vacuum starts at whichever trigger is reached first, so key the read on
`n_ins_since_vacuum` near 4.9M (the insert trigger, 4,898,641) or `n_dead_tup`
near 2.05M (2,046,622), whichever comes first. The 4.9M shifts once the first
vacuum changes `relallfrozen` and `reltuples` (20% frozen gives about 4.09M, 30%
about 3.58M; arithmetic), so compute both live from `pg_class`: `SELECT 1000 +
0.05 * reltuples * (1 - relallfrozen::float8 / NULLIF(relpages, 0)), 50 + 0.02 *
reltuples FROM pg_class WHERE oid = 'fact_records'::regclass` (reviewer-run on a scratch table only, not on QA).
Arithmetic from the one hour after the first vacuum (52.1 dead rows and 19.7
inserts per second): the dead-row trigger comes first, about 10.9 hours after
the vacuum ended, and the insert trigger at about 69 hours. An earlier version
of this note said the insert trigger comes first, 17 to 29 hours after a
completed vacuum; that used the baseline rates (14 to 21 dead and 47 to 80
inserts per second) and does not hold at the rates measured since. The order
depends on the rates: at 14 dead rows and 80 inserts per second the insert
trigger would come first, at 17 hours. So key on whichever counter is nearer its
trigger, computed live; do not assume the order. An operator who waits only for
`n_dead_tup` near 2.05M can miss the read when inserts lead. Before 278 has
rolled, the triggers are the server defaults and none of these figures apply.

Local proof for the change itself (disposable PostgreSQL 18.6, migration run
through the shipped runner): `pg_class.reloptions` for `fact_records` is empty
before migration 278 and holds exactly the two parameters after; the relation
file is unchanged (catalog-only, no rewrite); a restart skips it by ledger
receipt and a ledger-less replay leaves the options identical. That is a
behavior proof, not a vacuum-duration claim.

## Observability Evidence:

No new metric or span is added. An operator at 3 AM reads the result from
signals that already exist:

- `pg_stat_progress_vacuum` is the only live view of a running pass (phase,
  heap blocks scanned out of total, index vacuum count), but only for a
  superuser or a role with `pg_read_all_stats`. For other roles the columns from
  `relid` on are NULL and a filter on the table returns no rows
  (reviewer-measured, see the roll-time check), so the `pg_locks` row is the
  live signal there. The log line and `last_autovacuum` appear only at the end.
- The autovacuum log line for `fact_records` (pages removed and remaining,
  tuples removed, dead-but-not-yet-removable count, removable cutoff, index
  stats, duration, buffer and WAL usage) appears for any run over 10 minutes.
  Read the second vacuum's line too; it prices the steady state.
- `pg_stat_user_tables` for `fact_records`: `n_dead_tup`, `n_ins_since_vacuum`,
  `last_autovacuum`, `autovacuum_count`. A healthy table now shows
  `n_dead_tup` resetting near 2% of live rows instead of climbing past 14M.
- `pg_class.reloptions` for `fact_records` confirms the migration applied.
- The keyed tick durations and watermark gauge show whether the ticks move.
  The per-phase tick histogram that would split them was merged in #9981 and
  is not part of this change.
- Replica slot lag and primary CPU throttling, read before, during, and after
  the first vacuum, since WAL volume and CPU rise during the pass.
- Read-side effect: `eshu_dp_postgres_reader_stage_duration_seconds` with
  `role="reader"`, `stage="reader_replay"`, `outcome="deadline"`, and
  `pg_stat_database_conflicts` on the replica (see the operator section below).

## Lock and rollout

`ALTER TABLE ... SET (autovacuum_*)` takes SHARE UPDATE EXCLUSIVE. That lock
does not conflict with INSERT, UPDATE, DELETE, or SELECT, and for autovacuum
parameters the table contents are not modified. It does conflict with the lock
a running autovacuum holds, and the PostgreSQL 18 maintenance page says so:
lock acquisition "will interrupt the autovacuum". So the ALTER does not wait
behind an ordinary autovacuum. It cancels it.

Measured by the independent review on PostgreSQL 18.6 with a slow autovacuum
on a scratch table (not on `fact_records`): the ALTER returned in 1,002.5 ms,
the `deadlock_timeout` of 1 s; the server log showed `canceling autovacuum
task`; a new worker restarted the table from block 0 within one naptime (that
cluster ran `autovacuum_naptime` 1 s; the default is 60 s). An identical second
ALTER did the same to the relaunched worker. A cancelled run reports nothing:
`autovacuum_count` stayed 0. In that run only an anti-wraparound worker
(`... (to prevent wraparound)`) made the ALTER wait; with `lock_timeout` 5 s it
failed with SQLSTATE 55P03 and the worker kept running. QA's total transaction
use is about 37.7M, far from the 200M default `autovacuum_freeze_max_age`, so
that case is not expected; the QA setting itself was not read.

Consequences for rollout:

- Applying the migration where no vacuum of `fact_records` is running is the
  clean case. A vacuum starts a naptime later only if the counters already
  exceed the new thresholds at apply time; after a completed vacuum they do not.
- An ALTER applied while a vacuum is running cancels it and restarts it from
  the beginning, with no warning in the schema Job log (only the Postgres server
  log says so). Reloptions stay unchanged, so a replay is harmless to the
  settings and costly to a running vacuum.
- A one-off ALTER and the later schema Job's receipt-less ALTER are the same
  statement, so a one-off that ran first would be re-issued by the Job and
  cancel a first vacuum still running. Run a one-off only if the roll is
  expected after the first vacuum finishes, or if the owner accepts a restart.
- Any other DDL on `fact_records` that takes the same lock does the same,
  including a separately rolled index migration (migration 274, from #9979,
  is a `CREATE INDEX CONCURRENTLY`). The PostgreSQL 18 explicit-locking page lists
  `VACUUM` (without `FULL`), `ANALYZE`, `CREATE INDEX CONCURRENTLY`, and
  certain `ALTER TABLE` variants as acquiring SHARE UPDATE EXCLUSIVE, and the
  routine-vacuuming page says "If a process attempts to acquire a lock that
  conflicts with the `SHARE UPDATE EXCLUSIVE` lock held by autovacuum, lock
  acquisition will interrupt the autovacuum."
- Roll-time check. Before a roll whose image carries migration 278 or any other
  `CREATE INDEX CONCURRENTLY` on `fact_records` (274, the #9979 index build, and
  #10035's migration 276, a partial index on the same table that was still an
  open PR when this was written), read:

  ```sql
  SELECT l.pid, a.backend_type, a.state
  FROM pg_locks l JOIN pg_stat_activity a USING (pid)
  WHERE l.relation = 'fact_records'::regclass
    AND l.mode = 'ShareUpdateExclusiveLock'
    AND l.granted;
  ```

  What the rows show depends on the role that runs the query. Reviewer-measured
  on PostgreSQL 18.6 with a running autovacuum on a scratch table named
  `fact_records`:

  | Read | Superuser | Table owner, not superuser |
  |---|---|---|
  | The query above | pid, `autovacuum worker`, `active` | pid, `backend_type` NULL, `state` NULL |
  | `pg_stat_progress_vacuum` filtered by the table | pid and phase | no rows |
  | `pg_stat_progress_vacuum`, all rows | not needed | `pid`, `datid`, `datname`; from `relid` on NULL |
  | `pg_stat_activity.query` for that pid | not needed | `<insufficient privilege>` |
  | `pg_stat_user_tables` counters | not needed | readable |

  Diagnosis 9.1 found that the `eshu` role on QA is not a superuser and not in
  `pg_read_all_stats`, so the right-hand column is the one to expect there; the
  role that runs a given roll check was not checked. Read the rows this way:

  - Any granted `ShareUpdateExclusiveLock` row on `fact_records` whose pid is
    not your own session, and not a known migration or `CREATE INDEX` session,
    means a hidden backend holds the lock, most likely an autovacuum worker.
    A session of your own role shows its type, so a schema Job's index build is
    recognisable. Superusers see `backend_type = 'autovacuum worker'`.
  - For the roll hold, treat a NULL-typed row as "vacuum running". Do not read
    a blank type, or an empty `pg_stat_progress_vacuum` result filtered by the
    table, as "no vacuum"; an empty unfiltered result does mean none.
  - A hidden pid listed in the unfiltered `pg_stat_progress_vacuum` is a vacuum.
    An analyze-only worker shows the same NULL row but is listed in
    `pg_stat_progress_analyze` instead, and `last_autoanalyze` moves. The #9805
    exclusion window opens on either row, which errs on the safe side.
  - Confirm with `last_autovacuum` and `autovacuum_count` before and after. A
    completed vacuum moves both; a cancelled one, or an analyze-only worker,
    moves neither.
  - The lock row alone cannot name the holder's type for such a role, and
    progress detail needs `pg_read_all_stats`.

  A worker means the roll cancels that vacuum and it restarts from block 0.
  Either wait for `last_autovacuum` to update, or roll and record the restart.
  After a completed vacuum the roll is clean. The query is the arbiter's
  recipe; the reviewer ran it as above. It reads the lock table, so it works
  where the progress view is hidden, but only as the hidden-holder test
  described here. Three gaps, each a reviewer measurement (PostgreSQL 18.6
  scratch tables, naptime 1 s):
  - A roll carrying 274 (the #9979 index build) and 278 does not restart the
    vacuum within a naptime. A session lock stood in for the build (not a real
    `CREATE INDEX CONCURRENTLY`): with the trigger exceeded, the server logged
    `skipping vacuum ... lock not available` each naptime and no worker ran
    until the holder ended. Dead rows keep growing; 274 on QA was not checked.
  - The row outlives the vacuum: after `autovacuum_count` moved, the same pid
    held it through the post-vacuum ANALYZE, and an ALTER then cancelled only
    the ANALYZE (`vac=1 an=0`). The log still read `canceling autovacuum task`
    with vacuum context, so trust `autovacuum_count`, not the row or that line.
  - The read is point-in-time. Rerun it immediately before the roll, and
    compare `autovacuum_count` and the pid after it.
- If the lock is blocked by an anti-wraparound worker, a manual VACUUM or
  ANALYZE, or any session holding a conflicting lock (a concurrent index build
  is one), the runner's per-statement `lock_timeout` (5 s) and 3-minute retry
  budget apply; past that the Job fails closed and a re-sync recovers it.
  `pg_locks` names the holder.

There is no reverse migration in this change. The stop procedure is in the
operator checklist for the first vacuum
([9975-fact-records-first-vacuum-runbook.md](9975-fact-records-first-vacuum-runbook.md)),
which also covers what to watch, the recurring cost, and the WAL retention
check.

## First vacuum on QA (natural, default settings): measured 2026-11-04

Source: diagnosis section 12 (12.1 to 12.10), read by the diagnosis lane on the
QA primary; nothing was changed on QA. This vacuum ran under the server's
default thresholds, before migration 278 had rolled. It prices one pass and
tests the dead-tuple theory; it is not a measurement of migration 278.

| Field | Value | Source |
|---|---|---|
| T0 (autovacuum worker start) | 09:00:52.7Z, derived from the log's elapsed time; the 5-minute watcher gap was 08:56:18Z to 09:01:19Z | watcher and log |
| Duration (T0 to `last_autovacuum`) | 6,151.92 s (1 h 42 min 32 s); ended 10:43:24.607Z | log |
| Counters at first sighting, 09:01:19Z | `n_ins_since_vacuum` 19,600,637 (default trigger 19,591,565); `n_dead_tup` 15,266,142; `autovacuum_count` not in the measurement file | watcher |
| Log line (the run exceeded 10 minutes) | 8,548,139 of 21,350,368 pages scanned (40.04%); 1 index scan; 15,858,845 dead item identifiers removed; 1,500,592 tuples removed by the pass; 1,364,710 tuples frozen; WAL 51,722,778,115 bytes (48.2 GiB) with 11,650,515 full-page images; 23,174,029 buffer reads, 11,770,455 dirtied; I/O read 719.4 s, write 60.6 s; CPU user 505.8 s, system 156.3 s. The verbatim line stays in the diagnosis raw files. | log |
| Dead but not yet removable, removable cutoff | 8,736; cutoff 109,356 XIDs old at the end; `n_dead_tup` 13,333 after | log and stats |
| Buffers per row, repeat pass (cold) | 1.4 (2.3) right after, 717 buffers and 0 reads; 1.3 an hour later, 680 buffers. Control before the vacuum: 7.7 (13.1) at 05:54Z; earlier 12.2 at 04:21Z and 35.4 with the holder open | probe |
| 197-row triple | before 5,552 TIDs, 5,452 heap blocks, 18 index buffers; after 474 TIDs, 466 heap blocks, 21 index buffers; repeat pass 294 buffers against 3,108 for the control; an hour later 524 TIDs, 511 heap blocks, 273 buffers | probe |
| Replica conflict peak and `reader_replay` deadline series | `confl_snapshot` 140 to 140 (peak delta 0); no `deadline` outcome; every API response 200, no 5xx | replica, metrics |
| WAL: slot `wal_status` and retained WAL | `reserved` at every read; retained WAL at most 3.6 MB during the run and 4.2 MB in the hour after | primary |
| Primary CPU and CFS throttled ratio | not in the measurement file (only the vacuum's own CPU above) | not read |
| Stop-condition readings | S1, S2 and S3 never fired; data volume free 959.4 GB to 958.1 GB; the `/readyz` and queue-claiming readings are not in the measurement file | watcher |
| Keyed tick p95 and peak watermark gauge | during the run 0.945 s over 203 ticks and 34.8 s; the hour after completion (10:43:25Z to 11:43:25Z) 0.79 s over 120 ticks and 24.04 s | metrics |

Proof limits:

- One probe pair per state (n = 1). The live row count moved from 506 to 522,
  so compare buffers per row, not totals.
- The control showed the repeat figure falling by itself, 12.2 to 7.7 in 93
  minutes before any vacuum. Read against 7.7, 1.4 is 82% lower and 1.3 is
  83% lower. The control was taken about 3 hours before T0; none is closer.
- Keyed p95 was already 2.98 s in 08:00Z to 09:00Z, under the 15 s bar before
  the vacuum, so the hour inside the bar does not separate the vacuum from the
  holder cancel's earlier effect. The probe is the discriminator.
- The watermark gauge peak is driven by the audit path: 193.8 s at 07:25Z to
  08:25Z with no vacuum running, against 34.6 s in the hour before this
  vacuum. The gauge peak is not stable by itself.
- The start time comes from the log's elapsed figure and a 5-minute poll. The
  cold-pass values and per-phase tick time were not attributed (the #9981
  histogram was not read).
- The post-vacuum rates (52.1 dead rows and 19.7 inserts per second) are one
  hour, and the dead-row rate is above the baseline range; whether it persists
  is unmeasured.

The control pair was one more run of the same 10-triple probe taken before the
vacuum started, so that hint accumulation and the vacuum can be told apart. The
whole run is excluded from the #9805 closing bar.

## NOT_CHECKED

- A second vacuum's duration, WAL volume, API `503` rate, and the hot-row
  sawtooth size at its start. The first vacuum's are measured above (n = 1);
  later ones are not.
- For the first vacuum: hidden-backend xmins, the per-phase tick histogram
  (#9981), primary CPU and CFS throttling, and the `/readyz` and
  queue-claiming readings during the run were not read.
- Whether any other session holds an older snapshot; the first logged vacuum's
  removable cutoff is the test. `idle_in_transaction_session_timeout` and
  `transaction_timeout` were 0 on the QA primary, so a new holder can recur.
- Table ownership of `fact_records` under the runtime role on a deployed
  database. Migration 273 builds an index on it through the same schema Job,
  which implies the access; that is read from the migration, not from QA.
- The deployed PostgreSQL major version. The diagnosis read 18.3; the lock text
  is from the PostgreSQL 18 docs and the lock behavior was measured on 18.6.
- QA `deadlock_timeout`, `autovacuum_freeze_max_age`,
  `max_standby_streaming_delay`, and `max_slot_wal_keep_size`. If QA's
  `deadlock_timeout` were at or above 5 s, the ALTER would hit its
  `lock_timeout` first and take the retry path instead of cancelling.
- The stop procedure: RESET below and past the default triggers,
  `autovacuum_enabled = false`, its undo, and its behavior against an
  anti-wraparound run were measured only by the independent review on scratch
  tables (PostgreSQL 18.6, naptime 1 s; the owner role varied by run), not by the author
  and not on `fact_records`.
- The roll-time check on QA: the reading for a role without `pg_read_all_stats`
  rests on diagnosis 9.1 and the reviewer's scratch-table measurement. Which
  role runs a given roll check was not checked, and no read of QA's grants was
  made.
- QA's counters after the 11:44Z hour readback, and whether the 52.1 dead rows
  per second rate persists. The 10.9 hour and 69 hour figures rest on one hour.
- The second vacuum measurement is still owed. The note describes what to read
  shortly before the second vacuum starts (`n_ins_since_vacuum` near 4.9M or
  `n_dead_tup` near 2.05M, with the 10-triple probe and the keyed tick gauge),
  but no figure from that read exists yet and none is claimed here.
