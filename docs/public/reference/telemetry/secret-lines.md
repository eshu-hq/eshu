# Secret-Line Finalizer Signals

The hardcoded-secret side table has a bulk-load lifecycle like the content substring indexes (#7125). Bootstrap-index
skips the write-time derivation, then the secret-line finalizer rebuilds
`content_file_secret_lines` and publishes `content_file_secret_lines_state`
(`not_built`, `building`, `ready`, `failed`, plus an `epoch`). Watch it with
`eshu_dp_bootstrap_pipeline_phase_seconds{bootstrap_phase="secret_lines_finalization",collector_kind="bootstrap-index"}`,
`eshu_dp_secret_lines_backfill_files_total` (files re-derived; compare with the
`content_files` row count), and
`eshu_dp_secret_lines_backfill_batches_total{outcome="committed|retried"}` (a
rising `retried` share means the finalizer is yielding to live writers; zero
`committed` while the state is `building` means it is stuck). The log events are
`secret_lines.finalize_started`, `secret_lines.finalize_progress` (every 10 s),
`secret_lines.finalize_complete`, and `secret_lines.finalize_failed`. One
bootstrap-index at a time is enforced by a session advisory lock: a second run
logs `bootstrap.postgres.ownership.waiting` with `lock=bulk_load` (holder pid,
application name and connection age), then fails; the events
`secret_lines.bulk_load_lock_acquired` (`pid`, `waited_ms`, `polls`),
`secret_lines.bulk_load_lock_refused`, `secret_lines.bulk_load_lock_released`
(`held_seconds`) and `secret_lines.bulk_load_lock_release_failed` mark the
lock's life, and the refusal and release failure carry
`failure_class=secret_lines_bulk_load_lock_refused` and
`secret_lines_bulk_load_lock_release_failure`. Every
hardcoded-secret investigation read is counted in
`eshu_dp_hardcoded_secret_reads_total{source="side_table|legacy_scan"}` and the
span attribute `eshu.hardcoded_secret.read_source`; a sustained `legacy_scan`
rate after bootstrap finished means the finalizer failed and reads are paying the
full content scan.

The reducer repairs a state that is not `ready` (#8833). Each poll is counted in
`eshu_dp_secret_lines_repair_runs_total{outcome="skipped_ready|skipped_locked|published|superseded|cancelled|failed"}`.
`skipped_locked` means another session holds the run lock `(5318,1)` (a
bootstrap-index run or another reducer replica) or live bootstrap-index sessions
hold the deferred-writer lock `(5318,2)`; `cancelled` is a shutdown during a poll
(Warn `secret_lines.repair_cancelled` with the `error` text, the state stays
`failed`, the next start heals it); `superseded` means the epoch moved during
the rebuild and the next poll takes the new one. The log events are
`secret_lines.repair_skipped` (`reason=ready|locked|deferred_writers`, `lock=bulk_load|deferred_writers`, `holder`),
`secret_lines.repair_started` (`epoch`, `state`, `policy_version` once migration
234 adds it), `secret_lines.repair_complete` (`outcome`, `files`, `rows`,
`duration_seconds`), and `secret_lines.repair_failed`
(`failure_class=secret_lines_backfill_failure`, `next_retry_seconds`); the
rebuild itself emits the `secret_lines.finalize_*` events and backfill counters
above. A not-ready state with a run of `failed` outcomes keeps every read on the
legacy scan.

Lock-loss signals (#8833), all `failure_class=secret_lines_bulk_load_lock_lost`
except the Warn: `secret_lines.bulk_load_lock_lost` (ERROR, `pid`,
`held_seconds`) when the keep-alive on a pinned lock session fails, the session
and its lock are gone; `secret_lines.deferred_session_refused` (ERROR,
`run_lock_pid`) when a bootstrap-index connection is refused at birth because the
run's lock session is gone, and the write that needed it fails;
`secret_lines.finalize_lost_epoch` (ERROR) when bootstrap-index's finalizer
claimed nothing for its own epoch; `secret_lines.finalize_superseded` (Warn) when
it claimed nothing because a newer epoch exists. `secret_lines.bulk_load_lock_acquired`
carries `connection=pool|dedicated`: `dedicated` is the reducer's lock pool.
A bootstrap-index that starts during a reducer rebuild waits for `(5318,2)` on a probe
session and logs `bootstrap.postgres.ownership.waiting` and `.acquired` with
`lock=deferred_writers`; `secret_lines.deferred_writer_probe_release_failed` (Warn,
`error`) means the probe's unlock failed after the pool opened, which does not
affect the run.
