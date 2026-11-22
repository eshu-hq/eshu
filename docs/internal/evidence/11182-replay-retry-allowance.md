# Replay Retry Allowance Evidence (#11182)

The change gives a replayed projector row a new row's retry allowance. It adds
`fact_work_items.replay_attempt_baseline` (migration 334) and carries the
column through the projector claim. This note records why the claim path did
not get slower and how an operator sees the new state.

## No-Regression Evidence

No-Regression Evidence: the projector claim statement gains one column in the
`claimed` CTE's `RETURNING` list and in the final `SELECT`
(`projector_queue_claim_pool_sql.go`). The column is read from the row the
claim `UPDATE` already wrote, so it adds no relation access, join, sort, or
lock. The claim plan-shape guards pass unchanged at HEAD on a disposable
PostgreSQL 18.6 schema:

- `TestProjectorClaimFullGuardPlanShape` (PASS)
- `TestProjectorClaimMarkedGuardPlanShape` (PASS)
- `TestProjectorClaimSortedLockStepStopsAtFirstLockableRow` (PASS). This one
  pins `Limit -> LockRows -> Nested Loop` with no Sort above the join, and two
  loops for each lock-step probe.

The replay statements gain two assignments on rows they already lock and
update, so their lock set and lock order (`work_item_id COLLATE "C"`) do not
change. The concurrency proofs that pin that order pass:
`TestRecoveryReplay*Live` and `TestConcurrentReplaysMoveRowOnceLive` (eight
rounds per writer pair, exactly one replay moves the row each round).

Migration 334 adds a `NOT NULL DEFAULT 0` column and two nullable columns.
Each is a metadata-only change on PostgreSQL 11+, with no table rewrite and no
backfill, like migration 325 on the same table. The lock wait of the
`ADD COLUMN` on a production-size `fact_work_items` was not measured.

## Observability Evidence

Observability Evidence: this change adds no metric, span, or log field. The
retry and dead-letter counters keep their labels and their emit points in
`ProjectorQueue.Fail`. Those are `eshu_dp_queue_dead_letters_total`,
`eshu_dp_projector_retry_surge_total` and
`eshu_dp_projector_admission_wait_retries_total`. A replayed row that fails
again shows up on them exactly as a new row does.

The per-row allowance is durable state that an operator reads directly:

- `fact_work_items.replay_attempt_baseline`, with
  `attempt_count - admission_wait_attempts - replay_attempt_baseline` as the
  projection claims used since the last replay.
- `POST /api/v0/admin/work-items/query` and the replay response return
  `replay_attempt_baseline` per item.
- `POST /api/v0/admin/replay-events/query` returns `replay_attempt_baseline`
  and `prior_admission_wait_attempts` per replay event.

At 3 AM, an operator who sees a replayed row dead-letter again reads its row
and its replay event. They show how many claims the replay allowed and how many
admission waits the row had already spent.
