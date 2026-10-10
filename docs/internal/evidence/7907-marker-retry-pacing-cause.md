# #7907: pace write-marker deferral retries, refuse a missing fence, log the cause

## Problem

`MarkProjectionWriteStarted` (projector/write_marker.go) retried
`ErrWorkWriteMarkerDeferred` 150 times with no wait between attempts. Before
#7819 every deferral cost a ~2 s lock timeout, so the loop was self-pacing.
Since #7819 a busy claim fence defers in milliseconds, so sustained fence
contention burns all 150 attempts in under a second and the marker gives up
where waiting would have marked. Separately, a missing fence row (scope gone)
spun the same loop, and the `retried`/`gave_up` outcomes and the deferral WARN
could not tell a fence-busy deferral from a generation-row wait (review G1).

## What changed

- The loop waits out `writeMarkerDeferralBackoff` between deferrals: 10 ms,
  doubling per consecutive deferral, capped at 200 ms (about 29 s over a full
  bound of millisecond deferrals, inside the ~5 minute caller budget). The
  wait is ctx-aware: a cancel during the pace returns the shutdown outcome
  promptly instead of sleeping out the bound.
- The store tells a busy fence from a missing one with a lock-free existence
  read after the SKIP LOCKED miss. A missing row means the scope is gone (the
  trigger creates the fence with the scope; the scope delete cascades to the
  fence, generation, and work rows), so it refuses at once through
  `classifyWriteMarkerRefusal` — superseded for a gone scope — instead of
  deferring. A busy row defers wrapping the new
  `failure.ErrWorkWriteMarkerFenceBusy`, which wraps the general deferred
  sentinel so existing classifiers keep working.
- The deferral WARN carries `deferral_cause` (`fence_busy` or
  `generation_row`) and names the busy claim fence for a fence wait. Metric
  outcomes are unchanged.

## Proof

| Check | Before | After |
|---|---|---|
| `TestMarkProjectionWriteStartedPacesDeferrals` (3 immediate deferrals) | burns the bound in ms (no pacing) | waits 70+ ms, then marks |
| `TestMarkProjectionWriteStartedMissingFenceRefuses` (scope deleted) | `ErrWorkWriteMarkerDeferred` | `ErrWorkSuperseded` in 0.04 s, one attempt |
| `TestServiceWriteMarkerDeferralCauseIsLogged` | one message, no cause | `fence_busy` / `generation_row` surface distinctly |
| `TestMarkProjectionWriteStartedPaceHonorsCancel` | n/a (no sleep to cancel) | shutdown in 0.03 s, bound unburned |
| `TestMarkProjectionWriteStartedFenceSync/defers_on_busy_fence` | deferred (general) | deferred, pinning the fence-busy cause |
| projector + failure suites, incl. `-race` | GREEN | GREEN (0.26 s / 1.16 s race) |
| `verify-telemetry-coverage.sh` | GREEN | GREEN (no metric contract change) |

The pacing seam (`writeMarkerDeferralSleep`) defaults to a ctx-aware timer
and swaps atomically for tests; the package suite installs an instant sleeper
in TestMain so the 150-deferral exhaustion tests stay instant, while the
pacing tests swap the real sleeper back in.
