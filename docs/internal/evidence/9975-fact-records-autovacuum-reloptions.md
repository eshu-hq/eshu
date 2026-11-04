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
baseline for the vacuum is therefore the after-cancel pair, and the repeat
pass (12.2) is the cache-independent comparison. This is one before/after pair
per arm with no control run, so the cancel cannot be separated from hint
accumulation across passes. The two 2-row triples and the 197-row triple
stayed flat after the cancel; the 197-row one is unexplained. No vacuum has
run, so these figures say nothing yet about what vacuum will do.

The default triggers are scaled to 102M rows, so at today's rates (14 to 21
dead rows per second, 47 to 80 inserts per second) the table would take 3.3 to
4.8 more days to reach the dead-row trigger. Meanwhile the hot triples show
28 to 71 index entries per live row, so most heap visits find no visible row.

Computed triggers after this change (arithmetic from the same reltuples):

| Trigger | Formula | Value |
|---|---|---|
| Dead rows, scale factor 0.02 | 50 + 0.02 x 102,328,592 | 2,046,622 (about 2.05M) |
| Inserts, insert scale factor 0.05 | 1000 + 0.05 x 102,328,592 x 0.9572 unfrozen | 4,898,641 (about 4.90M) |

The insert formula that scales by the unfrozen fraction (`relallfrozen`) is
PostgreSQL 18 behavior. On PostgreSQL 13 to 17 the same arithmetic gives
1000 + 0.05 x 102,328,592 = 5,117,430, still exceeded.

Both are already exceeded, so the first vacuum starts within one
`autovacuum_naptime` (60 s) of the migration landing. At today's rates, later
vacuums follow every 1.1 to 1.7 days on dead rows and every 17 to 29 hours on
inserts, each removing about 2M row versions.

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

The run will exceed the 10-minute `log_autovacuum_min_duration` and so will be
logged. Expect the keyed ticks to run slower while it runs. Exclude the whole
run from the #9805 closing bar, from the first `pg_stat_progress_vacuum` row
until `last_autovacuum` updates, whatever its length.

Proof still owed after the migration reaches a database, on the same primary
and the same 10 triples: the first vacuum's own log line, `pg_stat_user_tables`
dead and insert counters, the probe again as total buffers per returned row,
and the keyed tick durations for the hour after the vacuum completes. Read it
against the after-cancel baseline (35.8 cold, 12.2 repeat), not against 46.0:

- Buffers per row near 1 to 2 on the repeat pass, and the hour inside the bar:
  the vacuum moved it past what the cancel and hints did, and the mechanism is
  closed.
- Buffers per row still high after a vacuum whose log line reports millions of
  rows "dead but not yet removable": a second snapshot holder caps it. Bring
  the removable cutoff from the log line to the arbiter.
- Buffers per row low but ticks still over 60 s: the tail has another
  component, most likely the write half, which was never measured. The
  per-phase tick histogram in open PR #9981 would name it.

One more read, shortly before the second vacuum starts (`n_dead_tup` near the
2.05M trigger): the same 10-triple probe plus the keyed tick gauge. It is a
single read, not a soak, and it is the point on the sawtooth that matters.

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
  heap blocks scanned out of total, index vacuum count). The log line and
  `last_autovacuum` appear only at the end.
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
- Replica slot lag and primary CPU throttling, read before and after the first
  vacuum, since WAL volume and CPU rise during the pass.
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
task`; a new worker restarted the table from block 0 within seconds. An
identical second ALTER did the same to the relaunched worker. A cancelled run
reports nothing: `autovacuum_count` stayed 0. Only an anti-wraparound worker
(`... (to prevent wraparound)`) made the ALTER wait; with `lock_timeout` 5 s it
failed with SQLSTATE 55P03 and the worker kept running. QA's total transaction
use is about 37.7M, far from the 200M default `autovacuum_freeze_max_age`, so
that case is not expected; the QA setting itself was not read.

Consequences for rollout:

- Applying the migration on a database where no vacuum of `fact_records` is
  running is the clean case. The first vacuum starts a naptime later.
- An ALTER applied while the first vacuum is running cancels it and restarts
  it from the beginning, with no warning in the schema Job log; only the
  Postgres server log says so. Reloptions stay unchanged, so a replay is
  harmless to the settings and costly to a running vacuum.
- The one-off ALTER on QA the ruling allows and the later schema Job's
  receipt-less ALTER are the same statement. If the one-off runs first, the
  Job re-issues it and will cancel a first vacuum that is still running. So run
  the one-off only if the roll is expected after the first vacuum finishes, or
  if the owner accepts a restart.
- Any other DDL on `fact_records` that takes the same lock does the same,
  including a separately rolled index migration (open PR #9979 adds one).
  Sequence such rolls with `pg_stat_progress_vacuum` in view.
- If the lock is blocked by an anti-wraparound worker or a manual VACUUM, the
  runner's per-statement `lock_timeout` (5 s) and its 3-minute retry budget
  apply; past that the Job fails closed and a re-sync recovers it.

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
- Live signal: read `pg_stat_progress_vacuum` for `fact_records`. It is the
  only view of a running pass.
- Stopping it. `pg_cancel_backend` on the worker alone does not stop it: the
  thresholds are still exceeded, and the review measured a new worker within
  one naptime. `ALTER TABLE fact_records RESET (autovacuum_vacuum_scale_factor,
  autovacuum_vacuum_insert_scale_factor)` should stop it, because by the lock
  rule above it cancels the running vacuum and puts the triggers back at 20%,
  above today's 14.6M dead rows and 17.4M inserts. That reasoning was not run
  on a table below the default trigger. It also leaves `pg_class.reloptions`
  different from the migration ledger, which will still show 275 applied and
  will not re-apply it. A forward migration is the way back into line. Whether
  the runtime role owns the table, or may signal an autovacuum worker, was not
  checked.
- Recurring cost. Each vacuum pass walks all 144 indexes (84 GB). Moving from
  about one vacuum every 3.3 to 4.8 days to one every 17 to 29 hours is
  roughly four times as many 84 GB index passes (about 96 h against about 23 h
  between runs at the midpoints; arithmetic), on a throttled primary,
  indefinitely. The first-run estimate above does not include this. Read the
  second run's log line to price the steady state.
- WAL retention. The replica's physical replication slot retains WAL while the
  replica lags, and this change sets no threshold. Before the first vacuum,
  read the slot's `wal_status` and retained WAL (`pg_wal_lsn_diff` between the
  current LSN and the slot's `restart_lsn`) and agree a stop value with the
  owner, for example stopping if `wal_status` leaves `reserved`. The QA
  `max_slot_wal_keep_size` was not read.

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
- A RESET on a real table below the default trigger.
