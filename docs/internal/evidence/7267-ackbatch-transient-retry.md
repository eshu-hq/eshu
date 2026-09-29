# #7267: transient AckBatch failures no longer cancel the reducer run

Root-Cause Evidence: Ifá fault-injection run 35943641519 (`expirelease` cell):
an unordered `UPDATE fact_work_items` deadlocked with AckBatch's ordered
`FOR NO KEY UPDATE`; the 40P01 reached `appendErr` in `runBatchConcurrent`,
which calls `cancel()`, and the reducer exited (drain residual stuck at 34).

## Change

`Service.ackBatchRetryingTransient` (`go/internal/reducer/service_observability.go`)
wraps `BatchWorkSink.AckBatch`. SQLSTATE 40P01 and 40001 are retried, at most 5
attempts total, with doubling backoff from 50 ms capped at 2 s. Any other error
is returned unchanged and still ends the run. An exhausted budget returns
`errAckAbandonedToLeaseExpiry`; the flush path records `ack_outcome_unknown`
and does NOT call `appendErr`, so the claims expire and are reclaimed.

Conflict domain: `fact_work_items` rows sharing a claim batch.
Lock order: unchanged. AckBatch still locks rows in `work_item_id` order.
Transaction scope: unchanged. A retry re-issues the whole AckBatch call.
Retry scope: idempotent because every ack statement is keyed by lease owner and
claim epoch (`last_attempt_at`). Rows already acked by a partially applied
earlier attempt, or reclaimed by another worker, match 0 rows and surface as
`ErrExecutionClaimRejected`, which is logged and never fatal. No row can be
double-completed.

## Proof

Test-First: `TestServiceRunBatchRetriesTransientAckFailure` (40P01, 40001) and
`TestServiceRunBatchAbandonsExhaustedTransientAckToLeaseExpiry` failed on the
unfixed flush path with `runMainLoop() error = batch ack reducer work ...
(SQLSTATE 40P01), want nil`, and pass with the change.
`TestServiceRunBatchStillFailsOnNonTransientAckError` pins that a
non-transient error is not retried.

No-Regression Evidence: the change adds no work to the success path (one
`AckBatch` call, no timer, no allocation beyond the existing slices). Worker
count, batch size and lease settings are unchanged.

Observability Evidence: WARN log `reducer batch ack hit transient failure;
retrying` with `failure_class=ack_transient_retry`, `attempt`, `batch_size`;
WARN log `reducer batch ack abandoned to lease expiry` with
`failure_class=ack_abandoned_to_lease_expiry`; abandoned items are recorded
through `recordReducerResult` as `ack_outcome_unknown`.

## Live contention proof

`TestReducerContentionGateAckBatchDeadlockRetryLive`
(`go/internal/storage/postgres/reducer_queue_ack_lock_order_postgres_live_test.go`,
enrolled in the reducer contention gate by its name prefix) runs
`reducer.Service.Run` with 2 workers and batch size 2 against the real
`ReducerQueue` as source and a recording wrapper around the real `AckBatch` as
sink. A blocker transaction locks the rows in the reverse of AckBatch's
`work_item_id` order and sets `deadlock_timeout` high, so Postgres always aborts
the ACK backend with a genuine 40P01.

- `retry_acks_each_row_once`: run returns nil; ACK calls are `[40P01, nil]`;
  both rows end `succeeded` with `attempt_count=1`; a replayed `AckBatch`
  returns `ErrReducerClaimRejected` and changes nothing.
- `retry_after_reclaim_rejects_stale_claim`: while the ACK backs off the blocker
  reclaims one row (new `last_attempt_at`, `attempt_count+1`); the retry matches
  0 rows for it and surfaces `ErrReducerClaimRejected`, the other row completes
  once, and the reclaimed row keeps its new claim.

Test-First: against `origin/main`'s `service_batch.go` (via `go test -overlay`,
tree untouched) both subtests fail with `reducer run ended on the deadlocked
ACK: ... (SQLSTATE 40P01)`; on the fix they pass, 10 of 10 with `-race`.

## Lock order and transaction scope

- `AckBatch` has no enclosing transaction: each domain group is its own
  autocommit statement, in this order: refresh-producer statements per domain,
  `container_image_identity`, `ci_cd_run_correlation`, generic.
- Inside each statement rows are locked `FOR NO KEY UPDATE` in
  `work_item_id COLLATE "C"` order (the CI/CD statement takes them row by row
  through an ordered CTE), then the UPDATE and the completion-event upsert run in
  the same statement. The ordering is unchanged by this fix.
- A 40P01 undoes only the statement that failed; earlier group statements of the
  same call stay committed.
- A retry repeats the whole `AckBatch`. Rows already acked are neither
  `claimed` nor `running` and hold no lease, and rows whose `last_attempt_at` or
  claim epoch moved are excluded, so a retry cannot complete a row twice or emit
  its completion event twice.
- Known cosmetic effect: in a mixed-domain batch where an early group committed
  and a later group deadlocked, the retry can log `reducer ack rejected stale
  claim` and record `ack_outcome_unknown` for items that in fact completed once.
  The data is correct; the telemetry over-reports uncertainty. Not changed here.

Unexplained, not reproduced: one earlier run on a stalled USB-backed Go cache
failed at the executor-wait step (`claims never reached the executor`) after
0.49 s; it did not recur in 10 runs on a local cache.
