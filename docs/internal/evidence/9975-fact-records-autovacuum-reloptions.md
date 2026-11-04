# #9975: per-table autovacuum triggers for fact_records

Issue #9975 (epic #9209, parent #9805) tracks the keyed winners tick breaching
its 60 s watermark bar. The read-only diagnosis in #9805 found that the probe
spends its time on index entries that point at heap locations with no visible
row. Dead row versions that vacuum has not yet removed are the leading
explanation; the diagnosis left open whether those entries are dead versions,
vacuumed-but-uncleaned line pointers, or something else. This note records why
migration 275 sets two autovacuum storage parameters on `fact_records`, what it
costs, how to read the result, and how to run the first vacuum safely.

## Classification

- Dead tuple pressure on a high-churn table (the reading, not an established
  fact): `fact_records` holds about 102.3M rows, and every update of an impact
  finding writes a new row version that is never HOT (the payload is indexed).
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
unexplained. No vacuum has run, so these figures say nothing yet about what
vacuum will do.

The default triggers are scaled to 102M rows, so at today's rates (14 to 21
dead rows per second, 47 to 80 inserts per second) the table would take 3.3 to
4.8 more days to reach the dead-row trigger. Meanwhile the hot triples show
28 to 71 index entries per live row, so most heap visits find no visible row.

The insert trigger is much closer, and it matters for rollout. Arithmetic,
unmeasured: diagnosis section 11.2 read `n_ins_since_vacuum` at 17,632,252 at
04:21:55Z on 2026-11-04. The default insert trigger is 19,591,565, so 1,959,313
inserts remain. At 80 and at 47 inserts per second that is 6.8 to 11.6 hours,
which is about 11:00Z to 16:00Z on the same day. The rates varied between
diagnosis windows (0.5 to 80.6 inserts per second), so treat the window as
indicative. The consequence: with no migration and no one-off at all, a
default-settings autovacuum of `fact_records` may start on its own once that
counter crosses its default trigger. A first vacuum seen before the migration
rolls is then a measurement of the theory with default settings, and a later
roll's ALTER would cancel it if it is still running (the rollout-order
consequence below applies equally). Read `last_autovacuum`,
`autovacuum_count`, and the roll-time lock check below before any roll.

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
still exceed the new thresholds at apply time. On QA a default-settings vacuum
is expected to start on its own about 11:00Z to 16:00Z on 2026-11-04 (the
arithmetic above, from counters read at 04:21Z; to be observed, not assumed).
Once that vacuum completes, the counters reset, so a roll that applies 275
afterwards starts no immediate vacuum. At today's rates the next
275-triggered vacuum then follows about 17 to 29 hours after a completed vacuum
on inserts (4,898,641 inserts at 80 and 47 per second; arithmetic) and 1.1 to
1.7 days on dead rows (14 to 21 per second), each removing about 2M row
versions.

"Dead rows near 2% of live" is the table-wide average, not what the probe
pays. The probe reads hot rows, which carry 28 to 71 index entries per live
row against a table-wide dead share of about 14%. Those entries built up over
about 94 hours, one version per 1.3 to 3.3 hours; the live-row xmin ages in
diagnosis section 9.2 say 0.4 to 1.9 hours. A 17 to 29 hour cycle therefore
rebuilds about 5 to 22 dead entries per live row on a hot row before the next
vacuum (17 / 3.3 to 29 / 1.3), or about 9 to 72 using the xmin ages
(17 / 1.9 to 29 / 0.4). This is arithmetic, unmeasured, and kill hints should
lower it by an amount nobody has measured. It means a sawtooth: near 1 to 2
buffers per row just after a vacuum, climbing back toward a share of today's
figure before the next one. The hour readback after the first vacuum reads the
bottom of that sawtooth, so it does not show that the 60 s bar holds a day
later (see the extra read below).

Expected first-vacuum duration: UNMEASURED, and not a prediction. What the
inputs support is a range of arithmetic, not a number:

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

The run is expected to exceed the 10-minute `log_autovacuum_min_duration` and
so be logged; the 5.3-minute credit floor above is under it, so this is not
certain. If it finishes in under 10 minutes there is no log line, and
`last_autovacuum`, `autovacuum_count`, and the dead and insert counters are the
test instead. Expect the keyed ticks to run slower while it runs. Exclude the whole
run from the #9805 closing bar, from the first granted
`ShareUpdateExclusiveLock` row on `fact_records` held by a backend that is not
your own session (the roll-time check below) until `last_autovacuum` updates,
whatever its length.

Proof still owed after the first vacuum, on the same primary
and the same 10 triples: the first vacuum's own log line, `pg_stat_user_tables`
dead and insert counters, the probe again as total buffers per returned row,
the 197-row triple's index and heap split again (18 index buffers and 5,452
heap blocks before), CPU, throttling, and replica slot lag during the run
(not only before and after), and the keyed tick durations for the hour after
the vacuum completes. Read it against the after-cancel baseline (35.8 cold,
12.2 repeat), not against 46.0:

- Buffers per row near 1 to 2 on the repeat pass, and the hour inside the bar:
  the vacuum moved it past what the cancel and hints did, and the mechanism is
  closed.
- Buffers per row still high after a vacuum whose log line reports millions of
  rows "dead but not yet removable": a second snapshot holder caps it. Bring
  the removable cutoff from the log line to the arbiter.
- Buffers per row low but ticks still over 60 s: the tail has another
  component, most likely the write half, which was never measured. The
  per-phase tick histogram in open PR #9981 would name it.

One more read, shortly before the second vacuum starts: the same 10-triple
probe plus the keyed tick gauge. It is a single read, not a soak, and it is the
point on the sawtooth that matters. Once migration 275 has rolled, the second
vacuum starts at whichever trigger is reached first, so key the read on
`n_ins_since_vacuum` near 4.9M (the insert trigger, 4,898,641) or `n_dead_tup`
near 2.05M (2,046,622), whichever comes first. Arithmetic, unmeasured: at the
note's own rates the insert trigger comes first, 17 to 29 hours after a
completed vacuum, and by then dead rows are only about 0.9M to 1.3M (14 to 21
per second for 17 hours) up to 1.5M to 2.2M (29 hours), assuming the counter
starts near zero. An operator who waits for `n_dead_tup` near 2.05M can miss
the read. Before 275 has rolled, the triggers are the server defaults and none
of these figures apply.

Local proof for the change itself (disposable PostgreSQL 18.6, migration run
through the shipped runner): `pg_class.reloptions` for `fact_records` is empty
before migration 276 and holds exactly the two parameters after; the relation
file is unchanged (catalog-only, no rewrite); a restart skips it by ledger
receipt and a ledger-less replay leaves the options identical. That is a
behavior proof, not a vacuum-duration claim.

## Observability Evidence:

No new metric or span is added. An operator at 3 AM reads the result from
signals that already exist:

- `pg_stat_progress_vacuum` is the only live view of a running pass (phase,
  heap blocks scanned out of total, index vacuum count), but only for a
  superuser or a role with `pg_read_all_stats`. For other roles the columns are
  NULL and a filter on the table returns no rows (reviewer-measured, see the
  roll-time check), so the `pg_locks` row is the live signal there. The log
  line and `last_autovacuum` appear only at the end.
- The autovacuum log line for `fact_records` (pages removed and remaining,
  tuples removed, dead-but-not-yet-removable count, removable cutoff, index
  stats, duration, buffer and WAL usage) appears for any run over 10 minutes.
  Read the second vacuum's line too; it prices the steady state.
- `pg_stat_user_tables` for `fact_records`: `n_dead_tup`, `n_ins_since_vacuum`,
  `last_autovacuum`, `autovacuum_count`. A healthy table now shows
  `n_dead_tup` resetting near 2% of live rows instead of climbing past 14M.
- `pg_class.reloptions` for `fact_records` confirms the migration applied.
- The keyed tick durations and watermark gauge show whether the ticks move.
  The per-phase tick histogram that would split them is in open PR #9981 and
  is not in this change.
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

- Applying the migration on a database where no vacuum of `fact_records` is
  running is the clean case. A vacuum starts a naptime later only if the
  counters already exceed the new thresholds at apply time; after a completed
  vacuum they do not.
- An ALTER applied while a vacuum is running cancels it and restarts it from
  the beginning, with no warning in the schema Job log; only the Postgres
  server log says so. Reloptions stay unchanged, so a replay is harmless to
  the settings and costly to a running vacuum.
- A one-off ALTER and the later schema Job's receipt-less ALTER are the same
  statement. If a one-off ran first, the Job would re-issue it and cancel a
  first vacuum still running. So a one-off is only worth running if the roll
  is expected after the first vacuum finishes, or if the owner accepts a
  restart.
- Any other DDL on `fact_records` that takes the same lock does the same,
  including a separately rolled index migration (open PR #9979 adds a
  `CREATE INDEX CONCURRENTLY`). The PostgreSQL 18 explicit-locking page lists
  `VACUUM` (without `FULL`), `ANALYZE`, `CREATE INDEX CONCURRENTLY`, and
  certain `ALTER TABLE` variants as acquiring SHARE UPDATE EXCLUSIVE, and the
  routine-vacuuming page says "If a process attempts to acquire a lock that
  conflicts with the `SHARE UPDATE EXCLUSIVE` lock held by autovacuum, lock
  acquisition will interrupt the autovacuum."
- Roll-time check. Before a roll whose image carries migration 274 or 275,
  read:

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
  | `pg_stat_progress_vacuum`, all rows | not needed | the pid, every other column NULL |
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
    a blank type, or an empty `pg_stat_progress_vacuum` result, as "no vacuum".
  - Confirm with `last_autovacuum` and `autovacuum_count` before and after. A
    completed vacuum moves both; a cancelled one moves neither.
  - The lock row alone cannot name the holder's type for such a role, and
    progress detail needs `pg_read_all_stats`.

  A worker means the roll cancels that vacuum and it restarts from block 0.
  Either wait for `last_autovacuum` to update, or roll and record the restart.
  After a completed vacuum the roll is clean. The query is the arbiter's
  recipe; the reviewer ran it as above. It reads the lock table, so it works
  where the progress view is hidden, but only as the hidden-holder test
  described here.
- If the lock is blocked by an anti-wraparound worker, a manual VACUUM or
  ANALYZE, or any session holding a conflicting lock (a concurrent index build
  is one), the runner's per-statement `lock_timeout` (5 s) and its 3-minute
  retry budget apply; past that the Job fails closed and a re-sync recovers
  it. `pg_locks` names the holder.

There is no reverse migration in this change; see the stop procedure below.

## Running the first vacuum

A checklist for whoever owns the rollout. Everything about size, duration, and
WAL here is unmeasured.

- API and MCP reads can return `503` while the first vacuum runs, and more
  mildly during later ones. QA runs `hot_standby_feedback` off. Vacuum cleanup
  records replayed on a replica conflict with standby queries whose snapshots
  can still see the removed rows; replay waits up to
  `max_standby_streaming_delay`, then cancels those queries. The read-routing
  page describes the failure: a burst of `503` `backend_unavailable` points at
  replica recovery conflicts (see `docs/public/deployment/postgres-read-routing.md`).
  Watch `eshu_dp_postgres_reader_stage_duration_seconds{role="reader",stage="reader_replay",outcome="deadline"}`
  and `pg_stat_database_conflicts` on the replica.
- Live signal. As a superuser or a role with `pg_read_all_stats`,
  `pg_stat_progress_vacuum` for `fact_records` is the only view of a running
  pass. Diagnosis 9.1 found the progress columns NULL for `eshu`, and
  `delay_time` and the other columns need `pg_read_all_stats`, which QA does not
  grant that role. A grant is a change nobody has authorized here. To tell
  whether a hidden vacuum is still advancing, read, every 10 minutes:
  - The `pg_locks` row keeps the same pid. A new pid or no row means the pass
    ended or restarted; check `last_autovacuum` and `autovacuum_count`.
  - `n_dead_tup`, `pg_statio_user_tables` block counters for `fact_records`
    (`heap_blks_read`, `heap_blks_hit`, and `idx_blks_*`), and the WAL position
    (`pg_current_wal_lsn()`) keep moving.
  - Inference, not measured: those counters include the normal workload, so
    they show the table and cluster are active, not that this pid is. Only the
    pid holding the lock identifies the vacuum. `n_dead_tup` also rises with
    ongoing updates until the run reports at its end.
- Stopping it. `pg_cancel_backend` on the worker alone does not stop it: the
  thresholds are still exceeded, and the review measured a new worker within
  one naptime. Which of the next two steps works depends on the counters, so
  read `n_dead_tup` and `n_ins_since_vacuum` for `fact_records` first.
  - Below both default triggers (about 20.5M dead rows and 19.6M inserts since
    vacuum, the figures in the table above), `ALTER TABLE fact_records RESET
    (autovacuum_vacuum_scale_factor, autovacuum_vacuum_insert_scale_factor)`
    stops it. The ALTER cancels the running vacuum by the lock rule above and
    the restored 20% triggers are not met. Reviewer-measured on PostgreSQL 18.6
    (scratch table, 100k dead rows, 150k inserts, naptime 1 s): the vacuum was
    cancelled and no new one started over 8 s. Not run by the author, and not
    on `fact_records`.
  - Past the default insert trigger (or the dead-row one), RESET does not stop
    it. Reviewer-measured on the same setup with 100k dead rows and 500k
    inserts: the RESET cancelled the vacuum and a new worker was running within
    2 s and still at 8 s, because the restored defaults are already exceeded.
    The inserts-since-vacuum projection above says QA may be past this point
    from about 11:00Z to 16:00Z on 2026-11-04, so assume RESET will not hold.
    A natural default-settings vacuum has no reloptions to reset at all, so
    RESET has nothing to undo for it.
  - Past the default triggers the stop is `ALTER TABLE fact_records SET
    (autovacuum_enabled = false)`, and it works in both cases.
    Reviewer-measured, same setup: the vacuum was cancelled and no new one
    started over 8 s. Caveats: it does not disable anti-wraparound
    vacuums. The PostgreSQL 18 `CREATE TABLE` storage parameters page says, for
    `autovacuum_enabled`: "If false, this table will not be autovacuumed,
    except to prevent transaction ID wraparound."; the
    `autovacuum_freeze_max_age` entry on the vacuum configuration page says
    "even when autovacuum is otherwise disabled", and the `autovacuum` entry
    says "even when this parameter is disabled". The reviewer also measured it
    (non-superuser owner, scratch table, PostgreSQL 18.6): with
    `autovacuum_enabled = false` and `autovacuum_freeze_max_age = 100000`, after
    the table passed that age, a worker ran `autovacuum: VACUUM ANALYZE
    public.fact_records (to prevent wraparound)`. While the setting is on, the
    table gets no ordinary autovacuum at all, so dead rows and the insert
    counter keep growing. It must be undone with `ALTER TABLE fact_records RESET
    (autovacuum_enabled)` before the roll that carries 275, because 275's `SET`
    names only the two scale factors and leaves `autovacuum_enabled = false` in
    place; skip the undo and autovacuum stays off on the table through the roll.
    Reviewer-measured, same setup: the 275 `SET` of the two scale factors
    succeeded and left `autovacuum_enabled=false` in `reloptions`, and no worker
    ran 4 s later. `RESET (autovacuum_enabled)` succeeded, a worker was back
    in about 1 s (naptime 1 s) with the scale factors kept. So the undo restarts
    the vacuum within one naptime while a trigger is still exceeded; schedule it
    rather than treat it as routine. Record the stop in the rollout log: while
    it is in place `pg_class.reloptions` carries `autovacuum_enabled=false`,
    which the migration ledger does not describe.
  - A RESET of the two scale factors also leaves `pg_class.reloptions`
    different from the migration ledger, which will still show 275 applied and
    will not re-apply it. A forward migration is the way back into line.
  - Owner rights are enough for the `SET` and `RESET` above (reviewer-measured
    as a non-superuser table owner on a scratch table). Whether the runtime role
    owns `fact_records` on QA, or may signal an autovacuum worker, was not
    checked.
- Recurring cost. Each vacuum pass walks all 144 indexes (84 GB). The default
  cycle is insert-driven: 19,591,565 inserts at 80 and 47 per second is 2.8 to
  4.8 days between runs (arithmetic; the 3.3 to 4.8 days above is the time left
  to the dead-row trigger, not the cycle). Moving to one every 17 to 29 hours
  is roughly four times as many 84 GB index passes (about 92 h against about
  23 h between runs at the midpoints; arithmetic), on a throttled primary,
  indefinitely. The first-run estimate above does not include this. Read the
  second run's log line to price the steady state.
- WAL retention. The replica's physical replication slot retains WAL while the
  replica lags, and this change sets no threshold. Before the first vacuum,
  read the slot's `wal_status` and retained WAL (`pg_wal_lsn_diff` between the
  current LSN and the slot's `restart_lsn`) and agree a stop value with the
  owner, for example stopping if `wal_status` leaves `reserved`. The QA
  `max_slot_wal_keep_size` was not read.

## First vacuum on QA (natural, default settings): measurement pending

Placeholder. Nothing below has been measured; every cell stays empty until the
read that fills it has run. The vacuum is expected to start by itself, under
the server's default thresholds, about 11:00Z to 16:00Z on 2026-11-04 (see the
insert-trigger paragraph above). Its readbacks are the first-vacuum proof the
change owes. Fill each field from the matching read window and cite the source
of the figure.

| Field | Value | Read |
|---|---|---|
| T0 (first autovacuum worker holding SHARE UPDATE EXCLUSIVE on `fact_records`) | pending | start watcher |
| Duration (T0 to `last_autovacuum`) | pending | during and after |
| Counters at T0 (`n_dead_tup`, `n_ins_since_vacuum`, `autovacuum_count`) | pending | start watcher |
| Log line (verbatim; only if the run exceeded 10 minutes) | pending | after |
| Pages removed and remaining, tuples removed and remaining, dead but not yet removable, removable cutoff | pending | after |
| Buffers per row, cold and repeat, against the control pair | pending | before and after |
| 197-row triple index and heap split (before: 18 index buffers, 5,452 heap blocks) | pending | after |
| Replica conflict peak (`pg_stat_database_conflicts`) and `reader_replay` deadline series | pending | during |
| WAL: slot `wal_status` and retained WAL | pending | during |
| Primary CPU, CFS throttled ratio, and API 5xx rate during the run | pending | during |
| Stop-condition readings at each read (slot `wal_status`, data-volume free space, `/readyz`, queue claiming), even when none fired | pending | during |
| Keyed tick p95 and peak watermark gauge for the hour after completion | pending | after |

The control pair is one more run of the same 10-triple probe taken before the
vacuum starts, so that hint accumulation and the vacuum can be told apart. The
whole run is excluded from the #9805 closing bar. If the run finishes in under
10 minutes there is no log line, and the holder test is `n_dead_tup` right
after `last_autovacuum` updates.

## NOT_CHECKED

- The first vacuum's real duration, WAL volume, API `503` rate, and the
  hot-row sawtooth size. All are arithmetic or unmeasured.
- Whether any other session holds an older snapshot; the first logged vacuum's
  removable cutoff is the test. `idle_in_transaction_session_timeout` and
  `transaction_timeout` were 0 on the QA primary, so a new holder can recur.
- Table ownership of `fact_records` under the runtime role on a deployed
  database. Migration 273 already builds an index on this table through the
  same schema Job, which implies the Job's role has the access; that is read
  from the migration's existence, not from QA.
- The deployed PostgreSQL major version. The diagnosis read PostgreSQL 18.3;
  the lock text was confirmed on the PostgreSQL 18 maintenance and ALTER TABLE
  pages and the lock behavior was measured on 18.6.
- QA `deadlock_timeout`, `autovacuum_freeze_max_age`,
  `max_standby_streaming_delay`, and `max_slot_wal_keep_size`. If QA's
  `deadlock_timeout` were at or above 5 s, the ALTER would hit its
  `lock_timeout` first and take the retry path instead of cancelling.
- The stop procedure: RESET below and past the default triggers,
  `autovacuum_enabled = false`, its undo, and its behavior against an
  anti-wraparound run were measured only by the independent review on scratch
  tables (PostgreSQL 18.6, naptime 1 s, non-superuser owner), not by the author
  and not on `fact_records`.
- The roll-time check on QA: the reading for a role without `pg_read_all_stats`
  rests on diagnosis 9.1 and the reviewer's scratch-table measurement. Which
  role runs a given roll check was not checked, and no read of QA's grants was
  made.
- QA's current `n_dead_tup`, `n_ins_since_vacuum`, `last_autovacuum`, and
  `autovacuum_count`. The 11:00Z to 16:00Z window is arithmetic from a
  04:21:55Z read.
