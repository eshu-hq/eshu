# #9975: running the first vacuum on fact_records

Operator checklist for the first (and later) vacuum of `fact_records` after
migration 278. It is the companion to the evidence note
[9975-fact-records-autovacuum-reloptions.md](9975-fact-records-autovacuum-reloptions.md),
which holds the diagnosis, the trigger arithmetic ("Performance Evidence"), the
lock rule ("Lock and rollout"), and what was not checked. "The main note" below
means that file.

A checklist for whoever owns the rollout. The first natural vacuum on QA (server
defaults, before migration 278) has run once and is measured: 6,151.92 s
(1 h 42 min 32 s) from 09:00:52.7Z to 10:43:24.607Z on 2026-11-04, 48.2 GiB of
WAL, no replica conflict, no `503`. That is n = 1, one pass at one moment. A
later pass or another primary is not measured. The main note's "First vacuum on
QA" section holds the readings and their limits.

- API and MCP reads can return `503` while the first vacuum runs, and more
  mildly during later ones. QA runs `hot_standby_feedback` off. Vacuum cleanup
  records replayed on a replica conflict with standby queries whose snapshots
  can still see the removed rows; replay waits up to
  `max_standby_streaming_delay`, then cancels those queries. The read-routing
  page describes the failure: a burst of `503` `backend_unavailable` points at
  replica recovery conflicts (see `docs/public/deployment/postgres-read-routing.md`).
  Watch `eshu_dp_postgres_reader_stage_duration_seconds{role="reader",stage="reader_replay",outcome="deadline"}`
  and `pg_stat_database_conflicts` on the replica. During QA's first vacuum the
  conflict peak delta was 0, no `deadline` outcome appeared, and every API
  response was 200 (n = 1).
- Live signal. As a superuser or a role with `pg_read_all_stats`,
  `pg_stat_progress_vacuum` for `fact_records` is the only view of a running
  pass. Diagnosis section 12 (9.1 earlier) found the progress columns NULL for `eshu`, and
  `delay_time` and the other columns need `pg_read_all_stats`, which QA does not
  grant that role. A grant is a change nobody has authorized here. To tell
  whether a hidden vacuum is still running, read, every 10 minutes:
  - The `pg_locks` row keeps the same pid. A new pid or no row means the pass
    ended or restarted; check `last_autovacuum` and `autovacuum_count`. A steady
    pid shows the worker is alive, not that the vacuum is advancing: the
    reviewer saw the same pid hold the row after `autovacuum_count` moved (0
    rows in `pg_stat_progress_vacuum`, 1 in `pg_stat_progress_analyze`), and a
    wedged worker should look the same (inference, not
    measured). Without `pg_read_all_stats`, a pass's own progress is not
    readable; its end shows in `last_autovacuum`, `autovacuum_count` and, past
    10 minutes, the log line.
  - `n_dead_tup`, `pg_statio_user_tables` block counters for `fact_records`
    (`heap_blks_read`, `heap_blks_hit`, and `idx_blks_*`), and the WAL position
    (`pg_current_wal_lsn()`) keep moving.
  - Inference, not measured: those counters include the normal workload, so
    they show the table and cluster are active, not that this pid is. Only the
    pid holding the lock identifies the worker; `n_dead_tup` also rises with
    ongoing updates.
- Stopping it. `pg_cancel_backend` on the worker alone does not stop it: the
  thresholds are still exceeded, and the review measured a new worker within
  one naptime. Which of the next two steps works depends on the counters, so
  read `n_dead_tup` and `n_ins_since_vacuum` for `fact_records` first.
  - Below both default triggers (after the first vacuum on QA, about 19.3M dead
    rows and 16.0M inserts since vacuum; they were 20.5M and 19.6M before it, and
    `reltuples` moves them, so compute them live as the main note's Performance
    Evidence section shows), `ALTER TABLE fact_records RESET
    (autovacuum_vacuum_scale_factor, autovacuum_vacuum_insert_scale_factor)`
    stops it. The ALTER cancels the running vacuum by the lock rule in the main note's "Lock and rollout"
    section and
    the restored 20% triggers are not met. Reviewer-measured on PostgreSQL 18.6
    (scratch table, 100k dead rows, 150k inserts, naptime 1 s): the vacuum was
    cancelled and no new one started over 8 s. Not run by the author, and not
    on `fact_records`.
  - Past the default insert trigger (or the dead-row one), RESET does not stop
    it. Reviewer-measured on the same setup with 100k dead rows and 500k
    inserts: the RESET cancelled the vacuum and a new worker was running within
    2 s and still at 8 s, because the restored defaults are already exceeded.
    On QA the first vacuum started at 09:00:52.7Z already past the default insert
    trigger, so RESET would not have held then. It ended at 10:43:24.607Z and
    reset the counters, so which case applies depends on the counters you read,
    not on the date.
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
    (autovacuum_enabled)` before the roll that carries 278, because 278's `SET`
    names only the two scale factors and leaves `autovacuum_enabled = false` in
    place; skip the undo and autovacuum stays off on the table through the roll.
    Reviewer-measured, same setup: the 278 `SET` of the two scale factors
    succeeded and left `autovacuum_enabled=false` in `reloptions`, and no worker
    ran 4 s later. `RESET (autovacuum_enabled)` succeeded, a worker was back
    in about 1 s (naptime 1 s) with the scale factors kept. So the undo restarts
    the vacuum within one naptime while a trigger is still exceeded; schedule it
    rather than treat it as routine. Record the stop in the rollout log: while
    it is in place `pg_class.reloptions` carries `autovacuum_enabled=false`,
    which the migration ledger does not describe.
  - A RESET of the two scale factors also leaves `pg_class.reloptions`
    different from the migration ledger, which will still show 278 applied and
    will not re-apply it. A forward migration is the way back into line.
  - Owner rights are enough for the `SET` and `RESET` above (reviewer-measured
    as a non-superuser table owner on a scratch table). QA's `fact_records` owner
    is `eshu` (diagnosis section 12.1). Whether the role that runs a stop may
    signal an autovacuum worker was not checked.
- Recurring cost. Each vacuum pass walks all 144 indexes (84 GB). The first
  pass (1 index scan, 1 h 42 min, 48.2 GiB of WAL) is the only price measured.
  The cycle times below are arithmetic from one hour of post-vacuum rates on QA
  (52.1 dead rows and 19.7 inserts per second), and each ratio assumes those
  rates persist. At the default triggers (about 19.29M dead rows and 15.97M
  inserts after the first vacuum) the dead-row path is first, at about 103
  hours between runs. With 278's triggers (1,928,622 dead rows, 3,992,078
  inserts, from the 10:45Z `pg_class` reading) it is first at about 10.3 hours,
  and the insert path would take 56. That is about ten times as many 84 GB index
  passes, because the dead path moves from 0.20 to 0.02 of the table, on a
  throttled primary, indefinitely. The earlier estimates here (insert-driven, 2.8 to 4.8 days
  against 17 to 29 hours, "roughly four times") used the pre-vacuum insert rates
  of 80 and 47 per second and are replaced. Read the second run's log line to
  price the steady state.
- WAL retention. The replica's physical replication slot retains WAL while the
  replica lags, and this change sets no threshold. Before the first vacuum,
  read the slot's `wal_status` and retained WAL (`pg_wal_lsn_diff` between the
  current LSN and the slot's `restart_lsn`) and agree a stop value with the
  owner, for example stopping if `wal_status` leaves `reserved`. QA's
  `max_slot_wal_keep_size` is 384 GiB (diagnosis section 12.1), and the slot
  stayed `reserved` with at most 4.2 MB retained through the first vacuum.
