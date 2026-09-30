# #7444: single-item Ack retries transient failures, and abandoned acks get their own status

Root-Cause Evidence: #7267 fixed the run-kill only on the batch path. On the
single-item path `ackReducerWork` called `WorkSink.Ack` once and returned any
error to `executeWithTelemetry`, which `runSequential` and `runPerItemConcurrent`
both return to `Service.Run`, which cancels the shared context. Before the
change the new tests failed with `Run() error = ack reducer work: ...
(SQLSTATE 40P01), want nil` on both paths.

## Which configurations reach the single-item path

- `Workers <= 1` selects `runSequential`, which acks through `ackReducerWork`.
  The default worker count is `UsableCPUs()` (capped at 4 on Neo4j) in
  `loadReducerWorkerCount` (`go/cmd/reducer/config.go`), so a 1-CPU pod runs
  `Workers=1` unless `ESHU_REDUCER_WORKERS` says otherwise.
- `Workers > 1` reaches it through `runPerItemConcurrent` only when the sink does
  not implement `BatchWorkSink`. The production `ReducerQueue` does, so that is a
  test-double case.
- No committed configuration pins `Workers <= 1`: the compose files default to 4
  and `scripts/verify-ifa-fault-injection.sh` pins `ESHU_REDUCER_WORKERS=4`. The
  `faultreplay` runner tests use `Workers: 1`, but they are hermetic and never
  ack through Postgres.

So the gap is real for a 1-CPU deployment and for an operator who sets the worker
count to 1, and no committed live gate exercises it.

## Change

- `Service.retryTransientAck` (`go/internal/reducer/service_observability.go`)
  is the one retry loop. `ackBatchRetryingTransient` and the new
  `ackSingleRetryingTransient` both call it, so the two paths cannot drift. The
  classifier (`isTransientAckError`: SQLSTATE 40P01 and 40001), the budget (5
  attempts) and the backoff (50 ms doubling, 2 s cap) are unchanged, and the batch
  log text is unchanged.
- `ackReducerWork` (`go/internal/reducer/service_batch.go`) now acks through the
  retry. An exhausted budget records `ack_abandoned_to_lease_expiry` and returns
  nil, so the run keeps draining; a shutdown that interrupts a backoff
  (`errAckRetryInterrupted`, wrapping the context error) records
  `ack_outcome_unknown` and returns nil, where before the same 40P01 ended the
  run with an `ack reducer work` error (new behavior, single-item path only; the
  batch path already stopped cleanly on shutdown); every other error still
  returns `ack reducer work: ...` with `ack_failed`.
- `runSequential` (`Workers <= 1`) returns nil after a fully handled iteration once
  the context is cancelled. Without it the loop went straight back into `Claim`,
  which hands the cancelled context to the database, so a shutdown during an ack
  retry ended the run with `claim reducer work: context canceled` instead of a
  clean stop. The test stub's `Claim` ignores the context and hid this; a review
  found it, and `TestServiceSingleAckShutdownDuringBackoffStopsSequentialRunCleanly`
  uses a context-respecting source and fails on the previous head with that claim
  error. `ackStatusOutcomeUnknown` is now one constant shared by the recorder, the
  log switch and the ack paths.
- The batch flush records `ackFailureStatus(err)`, so batch abandonment also gets
  the new status while shutdown and non-transient failures keep
  `ack_outcome_unknown`.
- `recordReducerResult` has an explicit case for the new status. Without it the
  `default:` arm would log `reducer execution succeeded` for an abandoned ack.

Conflict domain: the `fact_work_items` row of the one intent this worker holds
the claim for. Lock order and transaction scope are unchanged: each attempt is
its own `Ack` transaction and only the ack repeats, never the handler.

Retry scope: idempotent. The default single-item statement (`ackReducerWorkQuery`,
`go/internal/storage/postgres/reducer_queue_ack.go`) matches on `lease_owner`,
`status IN ('claimed','running')`, `claim_until > clock_timestamp()` and
`last_attempt_at`; the value-flow refresh variant keeps the same lease owner and
`last_attempt_at` predicate, and the `container_image_identity` variant adds an
explicit `container_image_identity_claim_epoch = $4`. A retry after a reclaim or after
the lease expired matches zero rows and returns `ErrReducerClaimRejected`, which
`ackReducerWork` already records as `ack_claim_rejected` and never treats as
fatal. The heartbeat is stopped before the ack, so the lease is not extended
during backoff; the worst case is about 750 ms of backoff plus five statement
times against a one-minute lease.

## Proof

Test-First (all fail on `origin/main` `725fa1942`, all pass with the change):

- `TestServiceSingleAckRetriesTransientFailure` (40P01 and 40001, `Workers` 1 and
  2): `Run() error = ack reducer work: ... (SQLSTATE 40P01), want nil after a
  transient single ack failure`.
- `TestServiceSingleAckAbandonsExhaustedTransientToLeaseExpiry` (`Workers` 1 and
  2): same error; it asserts exactly `2 * ackRetryAttempts` Ack calls, no `Fail`,
  only `ack_abandoned_to_lease_expiry:2` in `eshu_dp_reducer_executions_total`,
  and no `reducer execution succeeded` log.
- `TestServiceBatchAckAbandonedHasDistinctStatus`: `reducer execution statuses =
  map[ack_outcome_unknown:2], want only ack_abandoned_to_lease_expiry:2`.
- `TestServiceSingleAckShutdownDuringBackoffIsNotAbandonment`: also fails on the
  base commit, with `Run() error = ack reducer work: ... (SQLSTATE 40P01), want
  nil on shutdown`. It pins the new single-item shutdown behavior described
  above, and that shutdown is not recorded as abandonment.

Guards that pass before and after, pinning what must not change:
`TestServiceSingleAckStillFailsOnNonTransientError` (one Ack call, `ack_failed`,
run ends), `TestServiceSingleAckNonTransientErrorAtCancellationStillFailsRun` (a
non-transient error that lands during cancellation is still `ack_failed` and
still ends the run; it fails if the shutdown check is loosened to
`ctx.Err() != nil`, checked by mutation),
`TestServiceBatchAckShutdownDuringBackoffIsNotAbandonment` (batch shutdown stays
`ack_outcome_unknown`), and the existing `TestServiceRunLogsAckFailureWithQueueContext`.

Run: `go test ./internal/reducer -count=1` ok; the ack tests with `-race -count=30`
ok (157 s); `go test ./internal/replay/... ./cmd/reducer -count=1` ok.

No-Regression Evidence: worker count, batch size, lease settings, lock order and
transaction scope are unchanged. The success path adds a call through
`retryTransientAck` and one closure. Throwaway benchmark (not committed), Apple
M-series, 5 runs of 2 s each: direct `WorkSink.Ack` 7.6 to 7.9 ns/op, 0 allocs;
`ackSingleRetryingTransient` on success 34.8 to 35.1 ns/op, 0 allocs. About 27 ns
per ack against a Postgres statement measured in milliseconds.

Observability Evidence: a transient failure logs WARN `reducer ack hit transient
failure; retrying` with `failure_class=ack_transient_retry`, `attempt` and
`batch_size=1`. Abandonment logs WARN `reducer ack abandoned to lease expiry`
with `failure_class=ack_abandoned_to_lease_expiry`, then one WARN per item from
`recordReducerResult` (`status=ack_abandoned_to_lease_expiry`, `intent_id`,
`domain`), and counts each item in
`eshu_dp_reducer_executions_total{status="ack_abandoned_to_lease_expiry"}`. An
operator at 3 AM separates lease-expiry reclaim from shutdown by that status; the
`ack_outcome_unknown` status now means only shutdown or a partly committed batch.
No instrument is added, so the telemetry-coverage registry is unchanged; the
status is documented in `docs/public/reference/telemetry/metrics-reducer-storage.md`.

## Known limits

- Only SQLSTATE 40P01 and 40001 are transient; 55P03, 57014 and connection
  errors still end the run, as in #7267.
- No live Postgres proof for the single-item path. The 40P01 is injected through a
  fake `WorkSink`, and the `ifa-fault-injection` cells run 4 workers, which take
  the batch path. The epoch-keyed, zero-rows-after-reclaim behavior was proved
  live for `AckBatch` in `7267-ackbatch-transient-retry.md`; the single-item
  statement carries the same lease-owner and `last_attempt_at` predicate, checked
  by reading the SQL, not by a live run.
