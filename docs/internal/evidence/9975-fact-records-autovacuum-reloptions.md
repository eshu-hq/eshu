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
- Wrong trigger scale, not a slow vacuum: autovacuum last ran on 2026-10-31,
  3.9 days before the diagnosis read, because its default thresholds are 20% of
  the row count.
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
17,632,252 at 04:21:55Z on 2026-11-04, 1,959,313 short of the default insert
trigger (19,591,565). At 80 and at 47 inserts per second that is 6.8 to 11.6
hours, about 11:00Z to 16:00Z. The vacuum started earlier, at 09:00:52.7Z (see
the first vacuum section): reaching the trigger in 4 h 39 min took at least 117
inserts per second on average, above the 80.6 maximum the window used
(arithmetic). A bulk insert of about 774k rows landed between 06:30Z and 06:35Z
(diagnosis sections 12.7 and 12.8); after it the counter ran at 55 to 70 per
second. The consequence held: with no migration and no one-off, a
default-settings autovacuum of `fact_records` started on its own when that
counter crossed its default trigger. That first vacuum measures the theory with
default settings, not migration 278, and a later roll's ALTER would cancel one
that is still running (the rollout-order consequence below applies equally).
Read `last_autovacuum`, `autovacuum_count`, and the roll-time lock check below
before any roll.

Computed triggers after this change (arithmetic from the pre-vacuum `reltuples`;
the first vacuum moved it, see the recomputed triggers below):

| Trigger | Formula | Value |
|---|---|---|
| Dead rows, scale factor 0.02 | 50 + 0.02 x 102,328,592 | 2,046,622 (about 2.05M) |
| Inserts, insert scale factor 0.05 | 1000 + 0.05 x 102,328,592 x 0.9572 unfrozen | 4,898,641 (about 4.90M) |

The insert formula that scales by the unfrozen fraction (`relallfrozen`) is
PostgreSQL 18 behavior. On PostgreSQL 13 to 17 the same arithmetic gives
1000 + 0.05 x 102,328,592 = 5,117,430, still exceeded.

Both were exceeded at the baseline read, but a first vacuum starts within one
`autovacuum_naptime` (60 s) of the migration landing only where the counters
still exceed the new thresholds at apply time. On QA the default-settings
vacuum has now run, from 09:00:52.7Z to 10:43:24.607Z on 2026-11-04. Its end
reset the counters (`n_dead_tup` 13,333 and `n_ins_since_vacuum` 14), so a roll
that applies 278 afterwards starts no immediate vacuum. It also moved
`pg_class` (read at 10:45Z: `reltuples` 96,428,608, `relpages` 21,350,368,
`relallfrozen` 3,676,984), so 278's triggers on QA are no longer the ones in
the table above. Recomputed from that dated reading:

| Trigger | Formula | Default scale (0.2) | Under 278 |
|---|---|---|---|
| Dead rows | 50 + scale x 96,428,608 | 19,285,772 | 1,928,622 (scale 0.02) |
| Inserts | 1000 + scale x 96,428,608 x (1 - 3,676,984 / 21,350,368) | 15,965,313 | 3,992,078 (scale 0.05) |

A later autoanalyze can move `reltuples` again, so compute the triggers live
(the read below shows how). In the hour after the end the diagnosis reports
52.1 dead rows and 19.7 inserts per second over 3,640 s. The two counter
readings (13,333 and 14 at 10:45:21Z; 189,520 and 71,824 at 11:44:42Z, 3,561 s
apart) give 49.5 and 20.2 per second, because `n_dead_tup` rose by 176,187 in
that span. On those rates (arithmetic from one hour) the dead-row trigger is
10.3 to 10.8 hours after the end (1,928,622 divided by 52.1 and by 49.5) and the
insert trigger 55 to 56 hours (3,992,078 divided by 19.7 and by 20.2). The
dead-row trigger is first. These rates differ from the baseline ones (14 to 21
dead and 47 to 80 inserts per second), so read the hours as one hour's reading,
not a steady cycle.

"Dead rows near 2% of live" is the table-wide average, not what the probe
pays. The probe reads hot rows, which carry 28 to 71 index entries per live
row against a table-wide dead share of about 14%. Those entries built up over
about 94 hours, one version per 1.3 to 3.3 hours; the live-row xmin ages in
diagnosis section 9.2 say 0.4 to 1.9 hours. A 10.3 to 10.8 hour
cycle therefore rebuilds about 3 to 8 dead entries per live row on a hot row
before the next vacuum (10.3 / 3.3 to 10.8 / 1.3), or about 5 to 27 using the
xmin ages (10.3 / 1.9 to 10.8 / 0.4). This is arithmetic from the one-hour rate, and kill
hints should lower it by an amount nobody has measured. It means a sawtooth:
near 1 to 2 buffers per row just after a vacuum that ran an index scan
(measured: 1.4 right after the first vacuum and 1.3 an hour later), climbing back toward a share of today's
figure before the next one. The hour readback after the first vacuum reads the
bottom of that sawtooth, so it does not show that the 60 s bar holds a day
later (see the extra read below).

PostgreSQL can skip index vacuuming at 278's trigger, which changes the cost
per pass. The PostgreSQL 18 `VACUUM` page (`INDEX_CLEANUP`) says: "Normally,
`VACUUM` will skip index vacuuming when there are very few dead tuples in the
table." The default is `AUTO`. In the PostgreSQL 18 source (`lazy_vacuum`,
`BYPASS_THRESHOLD_PAGES` 0.02, `vacuumlazy.c`) a pass skips it when fewer than 2%
of the table's pages hold a dead item identifier and the dead-item store is
under 32 MiB. The log line then reads `index scans: 0` and `index scan bypassed:
N pages from table (X% of total) have M dead item identifiers`, `n_dead_tup`
resets, and the index entries stay until a later pass with an index scan. The
independent review measured it on a scratch table (PostgreSQL 18.6, 100,000
pages, 278's two settings): 8,732 dead rows on 1.75% of pages were bypassed,
a second batch at the same density (3.49% of pages) got an index scan, and the
same count at one per page (8.73%) got one at once.

On QA, 2% of 21,350,368 pages is 427,007 pages. The first vacuum's log line
gives 15,858,845 dead item identifiers on 2,982,877 pages, 5.32 per page. At
that density 1,928,622 dead rows touch about 362,753 pages, 1.70% of the table,
under the line (break-even 4.52 per page). So the second vacuum probably
bypasses. That is arithmetic from one vacuum's density, not observed, and the
density at a smaller count may differ. The 32 MiB condition looks clear (a
scratch reading scales to about 13 MB; coarse, not at scale). A bypassed pass
skips the walk of all 144 indexes and costs less. "About ten times as many
passes" counts triggered runs, not index scans, and index entries pile up
across bypassed passes until one crosses 2%.

First-vacuum duration: measured at 6,151.92 s (1 h 42 min 32 s). The arithmetic
made before the run was a range, never a prediction; it is kept short to show
how far it was off:

- Credit-bound floor. The full cost limit to one worker is 100,000 credits per
  second (miss 2, hit 1, dirty 20). Reading the heap plus 84 GB of indexes
  (about 10.25M pages) as all misses gives 10.3 minutes, or 5.3 minutes when the
  72.0% all-visible heap pages are skipped. Dirtied pages add 20 credits each
  (about 3.3 minutes per million). Sharing the limit among two or three workers
  (diagnosis section 9.1 saw two other vacuums running) gives 10.7 to 31 minutes.
- Read-latency bound. 0.571 ms per cold read over 16.0M to 31.0M reads is 2.5 to
  4.9 hours, an upper bound from a random-read figure; sequential reads should be
  faster.

Result against that arithmetic. The run (1.71 hours) sits between the two: far
above the credit-bound floors (5.3 to 31 minutes) and under the read-latency
range (2.5 to 4.9 hours). The pass read 23,174,029 buffers (I/O read time
719.4 s) and dirtied 11,770,455 pages. At 20 credits each the dirtied pages come
to about 235 million credits, or 2,354 s at the full unshared limit (38% of the
run); the reads add 463 s at 2 credits each, 46% together (arithmetic from the
credit model, all reads counted as misses). I/O read, I/O write (60.6 s) and CPU
(505.8 s user, 156.3 s system) add to 1,442 s of the 6,152 s. The rest is not
accounted for by these figures; waiting on the shared cost limit is the likely
cause (inference, not measured).

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
vacuum starts at whichever trigger is reached first. On the post-vacuum reading
above those are `n_dead_tup` 1,928,622 and `n_ins_since_vacuum` 3,992,078. Poll
both every 5 minutes and take the read when either is within 5% of its trigger
(dead rows above 1.83M, inserts above 3.79M); at 52.1 dead rows per second that
is about 31 minutes ahead. Waiting for the trigger itself misses it, since the
worker starts within a naptime of the crossing. Compute both triggers live
from `pg_class`, because a later autoanalyze moves them: `SELECT 1000 + 0.05 *
reltuples * (1 - relallfrozen::float8 / NULLIF(relpages, 0)), 50 + 0.02 *
reltuples FROM pg_class WHERE oid = 'fact_records'::regclass` (reviewer-run on a
scratch table only, not on QA). On the one hour's rates the dead-row trigger is
first, 10.3 to 10.8 hours after the 10:43Z end (roughly 21:00Z to 21:30Z on
2026-11-04, if 278 has rolled by then and the rates held), and the insert
trigger at 55 to 56 hours. The order depends on the rates: at the baseline 14
dead rows and 80 inserts per second the insert trigger would come first. So key on whichever counter is nearer its trigger; do not assume
the order. Before 278 has rolled, the triggers are the server defaults and none
of these figures apply.

Read the second vacuum's log line before the probe: `index scans:` and then
`index scan needed` or `index scan bypassed`.

- `index scans: 1`: read the probe against the first vacuum's 1.4 buffers per row.
- `index scans: 0` with `index scan bypassed`: PostgreSQL skipped index
  vacuuming, so a high probe says nothing yet about the mechanism while
  `n_dead_tup` reads near zero. Do not call the setting failed, and do not change
  it on one bypassed pass. Read the next vacuum, which does an index scan once
  dead-item pages pass 2% of the table. If passes keep bypassing and the probe
  stays high, take the log lines to the arbiter; forcing `vacuum_index_cleanup`
  on is a separate, unmeasured change.

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
that case is not expected. QA's `autovacuum_freeze_max_age` is the 200,000,000
default, and its `deadlock_timeout` is 1000 ms, below the runner's 5 s
`lock_timeout`, so the cancel path applies there (diagnosis section 12.1).

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
  migration 276, the terraform-backends outer-leg partial index on the same
  table), read:

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
  `pg_read_all_stats`, so the right-hand column is the one to expect there, and
  QA's own worker showed exactly that row at T0 (diagnosis section 12.8). The
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

Source: diagnosis section 12 (12.1 to 12.11), read by the diagnosis lane on the
QA primary; nothing was changed on QA. This vacuum ran under the server's
default thresholds, before migration 278 had rolled. It prices one pass and
tests the dead-tuple theory; it is not a measurement of migration 278.

| Field | Value | Source |
|---|---|---|
| T0 (autovacuum worker start) | 09:00:52.7Z, derived from the log's elapsed time; the 5-minute watcher gap was 08:56:18Z to 09:01:19Z | watcher and log |
| Duration (T0 to `last_autovacuum`) | 6,151.92 s (1 h 42 min 32 s); ended 10:43:24.607Z | log |
| Counters at first sighting, 09:01:19Z | `n_ins_since_vacuum` 19,600,637 (default trigger 19,591,565); `n_dead_tup` 15,266,142; `autovacuum_count` 39 before the run, 40 after | watcher |
| Log line (the run exceeded 10 minutes) | 8,548,139 of 21,350,368 pages scanned (40.04%); 1 index scan; 15,858,845 dead item identifiers removed; 1,500,592 tuples removed by the pass; 1,364,710 tuples frozen; WAL 51,722,778,115 bytes (48.2 GiB) with 11,650,515 full-page images; 23,174,029 buffer reads, 11,770,455 dirtied; I/O read 719.4 s, write 60.6 s; CPU user 505.8 s, system 156.3 s. The verbatim line stays in the diagnosis raw files. | log |
| Dead but not yet removable, removable cutoff | 8,736; cutoff 109,356 XIDs old at the end; `n_dead_tup` 13,333 after | log and stats |
| Buffers per row, repeat pass (cold) | 1.4 (2.3) right after, 717 buffers and 0 reads; 1.3 an hour later, 680 buffers. Control before the vacuum: 7.7 (13.1) at 05:54Z; earlier 12.2 at 04:21Z and 35.4 with the holder open | probe |
| 197-row triple | before 5,552 TIDs, 5,452 heap blocks, 18 index buffers; after 474 TIDs, 466 heap blocks, 21 index buffers; repeat pass 294 buffers against 3,108 for the control; an hour later 524 TIDs, 511 heap blocks, 273 buffers | probe |
| Replica conflict peak and `reader_replay` deadline series | `confl_snapshot` 140 to 140 (peak delta 0); no `deadline` outcome; every API response 200, no 5xx | replica, metrics |
| WAL: slot `wal_status` and retained WAL | `reserved` at every read; retained WAL at most 3.6 MB during the run and 4.2 MB in the hour after | primary |
| Primary CPU and CFS throttled ratio (5-minute windows) | during the run 8.44 to 11.68 cores, throttled ratio 0.093 to 0.780 (peak 10:18:50Z); the hour before the run 0.155 to 0.664, so the ranges overlap and the run's peak is higher; the hour after 5.6 to 11.0 cores, 0.006 to 0.531 | metrics |
| Stop-condition readings | S1, S2 and S3 never fired; data volume free 959.4 GB to 958.1 GB; `/readyz` 200 at 09:01:27Z and 11:45Z; reducer claim rate 9.28 to 11.44 per second with 594 to 854 pending during the run (9.8 to 11.3 and 603 to 869 in the hour after) | watcher, metrics |
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

## NOT_CHECKED

- A second vacuum's duration, WAL volume, API `503` rate, and the hot-row
  sawtooth size at its start. The first vacuum's are measured above (n = 1);
  later ones are not.
- For the first vacuum: hidden-backend xmins and the per-phase tick histogram
  (#9981) were not read.
- Whether any other session holds an older snapshot; the first logged vacuum's
  removable cutoff is the test. `idle_in_transaction_session_timeout` and
  `transaction_timeout` were 0 on the QA primary, so a new holder can recur.
- Whether the schema Job connects as the table owner, and whether that role
  may signal an autovacuum worker. QA's `fact_records` owner is `eshu`, the role
  the diagnosis session used (diagnosis section 12.1). That the Job runs as
  `eshu` is read from migration 273, which builds an index on the table through
  it, not from QA.
- The deployed PostgreSQL major version. The diagnosis read 18.3; the lock text
  is from the PostgreSQL 18 docs and the lock behavior was measured on 18.6.
- The stop procedure: RESET below and past the default triggers,
  `autovacuum_enabled = false`, its undo, and its behavior against an
  anti-wraparound run were measured only by the independent review on scratch
  tables (PostgreSQL 18.6, naptime 1 s; the owner role varied by run), not by the author
  and not on `fact_records`.
- The roll-time check on QA: QA's own worker showed the NULL-typed row at T0
  (diagnosis section 12.8: pid, NULL `backend_type`, NULL `state`, query
  `<insufficient privilege>`), so the reading for a role without
  `pg_read_all_stats` is observed there. Which role runs a given roll check was
  not checked, and QA's grants were not read.
- QA's counters after the 11:44Z hour readback, and whether the 52.1 dead rows
  per second rate persists. The 10.3 to 10.8 hour and 55 to 56 hour figures rest on one hour.
- Whether QA's second vacuum bypasses index vacuuming: 427,007 pages and 4.52
  per page are arithmetic from the first vacuum's density, and the 32 MiB
  condition was not measured at scale.
- The second vacuum measurement is still owed. The note describes what to read
  shortly before the second vacuum starts (`n_ins_since_vacuum` near 3.99M or
  `n_dead_tup` near 1.93M, with the 10-triple probe and the keyed tick gauge),
  but no figure from that read exists yet and none is claimed here.
