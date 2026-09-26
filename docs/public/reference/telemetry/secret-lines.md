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
