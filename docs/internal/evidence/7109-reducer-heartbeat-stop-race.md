# #7109: reducer heartbeat stop-versus-tick race no longer stops the process

## Problem

A reducer worker that finishes an intent while a periodic lease-heartbeat
UPDATE is still in flight cancels that UPDATE itself, through the stop function
`startHeartbeat` returns (`go/internal/reducer/service_heartbeat.go`). The
cancelled UPDATE returns `context canceled`, the heartbeat goroutine recorded it
as a heartbeat failure, and `executeWithTelemetry` (`service.go`) and
`executeAndReport` (`service_batch.go`) returned it as `ack_failed`. Both
callers treat that return as fatal: the per-item and batch runners cancel every
worker and `Run` returns the error, so `eshu-reducer` exits 1 for an item whose
handler succeeded. The projector heartbeat loop
(`go/internal/projector/service.go`) already guards the same race.

Root-Cause Evidence: the retained `resolution-engine.previous.log` of the
ops-qa pod shows `reducer lease heartbeat failed ... error="heartbeat reducer
work: heartbeat reducer work: context canceled"` at 00:20:56.000507, followed
by `reducer ack failed status=ack_failed handler_duration_seconds=30.021349622`
about 0.1 ms later in the log (00:20:56.000604). The handler ran 30.021 s
against a 30 s heartbeat interval, so it returned inside the first tick's round
trip. The error is `context canceled`, not a claim rejection, and no 40P01 or
lease-loss line precedes it. The reproducing tests in this change return exactly
that error string from `Run` on the base commit.

## Change

`startHeartbeat` sets an `atomic.Bool` (`stopping`) in the stop function
immediately before it cancels the heartbeat context. A periodic tick that
returns an error wrapping `context.Canceled` while `stopping` is set is dropped
and logged at debug level (`reducer lease heartbeat tick cancelled by stop;
ignored`); it is not counted as a missed heartbeat.

What still surfaces, each pinned by a test:

- Any tick error that is not `context.Canceled` (a database error, or a
  rejected claim) is recorded as before, so the item is not acked and the error
  stays fatal.
- A `context.Canceled` tick error when the stop function has not run, that is a
  parent-context cancellation, is recorded as before
  (`TestStartHeartbeatSurfacesParentCancelledTick`). Replacing the guard with
  the projector's `heartbeatCtx.Err() != nil` form makes that test fail, so the
  distinction between self-stop and parent cancel is exercised, not assumed.
- The immediate pre-heartbeat failure path is unchanged.
- A real tick failure is never cleared or replaced (round 2, below): the first
  real failure ends the tick loop.

## Round 2: a recorded real failure cannot be erased (review finding F1)

Hazard: tick N fails for a real reason, so the goroutine records the error and
calls `cancel()`. If the ticker channel already holds a queued tick (tick N took
at least `HeartbeatInterval` to fail), the next `select` sees both `ctx.Done`
and `ticker.C` ready and Go picks between them at random. Picking `ticker.C`
ran tick N+1 on the cancelled context. Once `stop` had set `stopping`, the
guard then did `done <- nil` and dropped the real failure; before `stopping`
was set, the follow-up tick's `context canceled` overwrote it. Either way
`stop()` returned nil or a bare `context canceled`, and an item with a real
heartbeat failure on record could be acked.

Fix: on the first real tick failure the goroutine records it, cancels, sends it
on `done` and returns. No follow-up tick can run, so nothing can clear or
replace the failure. Chosen over "first error wins" bookkeeping or a guard that
checks for a prior error because it removes the interleaving instead of
handling it, needs no extra state, and cannot change the healthy path. The
`ctx.Done` branch now sends nil, which is the same value it carried before: the
only writer of the old `heartbeatErr` was the tick branch that now returns.
Parent cancellation with no earlier failure still surfaces through the tick
that observes it (`TestStartHeartbeatSurfacesParentCancelledTick`).

Regression test:
`TestStartHeartbeatKeepsRealFailureWhenStopCancelsFollowUpTick`
(`service_heartbeat_first_failure_test.go`). The fake makes tick 2 outlast the
interval so a tick is queued when it fails, then answers later calls with
`context.Canceled`. The select choice is random, so each iteration hits the bug
about half the time; 64 iterations make a miss a 2^-64 event. On the round-1
head it failed 10 of 10 invocations, for example `iteration 3: stop() error =
heartbeat reducer work: context canceled, want the recorded real failure
reducer claim rejected`. With the fix it passed 20 of 20 invocations of the
heartbeat test set, and restoring the round-1 `service_heartbeat.go` makes it
fail again.

The ack itself remains the lease fence: `ReducerQueue.Ack` updates by intent
id, lease owner and claimed-at and returns `ErrReducerClaimRejected` when it
affects no row, so a lease that was actually lost is still rejected there.

## Conflict domain, concurrency and retry

- Conflict domain: one reducer work-item row per intent (its claim and lease),
  touched by the claiming worker's heartbeat UPDATE and its ack.
- Worker count, `HeartbeatInterval`, batch size, lease duration and claim SQL
  are unchanged. No serialization was added.
- The flag is written by the stop function before `cancel()` and read by the
  heartbeat goroutine after its UPDATE returns; `atomic.Bool` gives the ordering
  without a lock. Stop then waits on `done`, so the goroutine's decision is
  always observed. The race-detector run over `./internal/reducer` covers the
  new tests.
- Residual case, accepted: a parent cancellation that lands in the same window
  as a stop is indistinguishable from a stop. The worker is shutting down in
  that case, and the ack then runs under a cancelled context and fails as it
  did before this change.

## Verification

No-Regression Evidence: the change adds one atomic store in the stop function
and one atomic load on the periodic tick error path only. The healthy tick path,
claim, execute and ack paths are untouched. The queue and lease SQL do not
change, so before and after queue and row counts are identical by construction;
the reducer package tests, including the existing heartbeat tests, pass
unchanged.

Regression tests, `go/internal/reducer/service_heartbeat_stop_race_test.go`:

- `TestServiceRunAcksWhenHeartbeatTickIsCancelledByStop` covers sequential,
  per-item concurrent and batch-concurrent dispatch. The fake heartbeater holds
  the first tick until its context is cancelled and the handler returns as soon
  as that tick is in flight. On the base commit each variant fails with
  `Run() error = heartbeat reducer work: heartbeat reducer work: context
  canceled`; with the change each acks once and does not fail.
- `TestServiceRunStillSurfacesRealHeartbeatFailureAfterSuccessfulExecute` pins
  that a real tick failure is still returned and the item is not acked.
- `TestStartHeartbeatSurfacesParentCancelledTick` pins the parent-cancel
  distinction.

Observability Evidence: a real heartbeat failure keeps
`eshu_dp_reducer_heartbeat_missed_total` and the `reducer lease heartbeat
failed` error log with `failure_class=lease_heartbeat_failure`. The suppressed
stop race emits a debug log with the domain, intent id, worker id and phase, and
does not increment the missed-heartbeat counter, so that counter no longer
includes self-inflicted cancellations. Operators see a successful handler as
`succeeded` rather than `ack_failed` in the reducer result telemetry.
