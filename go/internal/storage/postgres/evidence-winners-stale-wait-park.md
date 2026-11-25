# Stale winners-watermark wait park

`WaitingDeferralMap` gains one row: `advisory_group_winners_watermark_stale`
parks on the `winners_watermark` wait kind with the existing 10-minute
backstop and the existing in-transaction resweep-commit wake
(`wakeWinnersWatermarkQuery`). Before, only the `not_ready` class parked, so
a stale verdict (30 minutes of post-claim deferral on a stuck watermark)
took the short exponential retry timer: every stale row re-claimed every few
seconds, starving the `supply_chain_impact` drain while a long winners
resweep held the watermark, then burned its 3 counting attempts and poisoned
as `dead_letter`.

No-Regression Evidence: the hermetic pair in
`reducer_queue_winners_stale_readiness_test.go` proves the queue contract —
`TestReducerQueueFailParksWinnersWatermarkStaleOnRetryableBudget` fails on the
unfixed tree (dead-letter branch: 1 query, 0 park execs) and passes with the
park (retrying, `visible_at = now + 10m`, 1 wait-row park exec plus the
satisfaction re-check), while
`TestReducerQueueFailDeadLettersWinnersWatermarkStalePastAttemptBudget`
passes on both trees, pinning that the stale wait stays counting and still
dead-letters past `MaxAttempts` when the maintainer is truly down. The live
drain proof `TestWinnersStaleWaitParksWakesAndDrainsLive` (disposable
bootstrap, PostgreSQL 18.6) fails on the unfixed tree at the park assertion
(`visible_at` horizon 2ms, zero wait rows) and passes with the fix through
the full loop: stale parks, a keyed tick stamps a covering watermark and
wakes the wait in-transaction with `reopened_at` aligned to the stamp, the
reclaim's bound sits under the stamp, and the production advisory-group
handler converges to success against the real winners reader with a stub
writer instead of re-queuing. `go test ./internal/storage/postgres/ -count=1`
is green except `TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline`,
which fails identically on the untouched base (missing `ESHU_DEPLOY_CHECKOUT`
external checkout, unrelated to this change);
`go test ./internal/reducer/advisory/group/ -count=1` is green, as are the
`WaitingDeferralMap` completeness tests. Baseline read-only snapshot from the
affected deployment: winners watermark 6.5h stale behind a single-flight
resweep that never committed, 877 of 880 `vulnerability_intelligence`
reducer dead letters in the stale class at `attempt_count` 3, 1305 not_ready
plus 296 stale advisory waits retrying, deferred outcomes ~0.053/s. After:
stale waits ride the 10-minute backstop and the resweep-commit wake, so a
slow-but-alive maintainer wakes its waiters instead of churning the retry
timer, and poison dead letters stop accumulating. Counting semantics are
unchanged (`attempt_count` still increments per claim, the `retryable` bound
is untouched), the wake predicate is unchanged (it already reopens every
retrying `winners_watermark` wait), and no lock, worker, batch, cadence, or
timeout knob changed.

Observability Evidence: no new telemetry. Parked stale waits surface through
the existing waiting set-aside rows, the existing
`eshu_dp_reducer_waiting_wake_total{site,outcome}` in-tx wake counter, the
existing `eshu_dp_reducer_executions_total{status,failure_class}` deferred and
dead-letter counters under the unchanged stale class, the existing watermark
age gauge, and the existing resweep skipped/committed log lines operators
already use to tell a slow resweep from a dead maintainer.
