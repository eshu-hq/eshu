# Identity Wait Start-Time Race Evidence (#7999)

`IdentityEpochCache.waitForFlight`
(`go/internal/storage/postgres/facts_active_container_image_identity.go`)
armed its wall-clock timer and only then read its start time. A clock advance
between the two was silently dropped from `waitedFor`, handing the caller
wait budget it had already spent. The symptom was
`TestIdentityEpochCacheWaiterWaitBudgetIsCumulative` failing about 1 run in
300 under `-race` with `waited = 0s, want 20s`, on branches that never touched
the identity cache.

## What changed

`started := c.clockNow()` now runs before the timer is armed, so the elapsed
wait covers the whole park. Timer duration, expiry, cancellation, and give-up
paths are unchanged. The regression test
`TestIdentityEpochCacheWaiterStartTimeReadBeforeTimerArmed` advances the fake
clock inside the timer constructor itself: strictly between the timer request
and the start read on the old order, strictly after the start read on the new
order. It fails deterministically on the old code and passes on the new code.

## Sibling fake-clock audit

- `TestIdentityEpochCacheWaiterWaitBudgetIsCumulative`: the flaky test. Its
  `awaitTimers(1)` observation now strictly follows the start read in program
  order, so the later `clock.advance` cannot precede it. Deterministic.
- `TestIdentityEpochCacheWaiterGivesUpAfterOneHeartbeatInterval`: asserts only
  the give-up outcome, never `waited`. Unaffected by the ordering.
- `TestIdentityEpochCacheWaiterCancelIsNotAGiveUp` and
  `TestIdentityEpochCacheLeaderCancelIsNotAGiveUp`: use the real clock and
  assert cancellation routing. No fake-clock ordering involved.

No-Regression Evidence: baseline is `origin/main` at `e9ed3ae9b9` plus the
one-commit fix on branch `fix/7999-identity-wait-start-time`. Backend is the
injected `fakeWaitClock` under `-race`; no Postgres or Neo4j is involved.
After measurement on the fix, in
`go/internal/storage/postgres`:

- RED: new test against the old order fails deterministically,
  `waited = 0s, want 20s` (`go test -race -count=1`).
- `go test -race -count=1000 -run
  '^TestIdentityEpochCacheWaiterStartTimeReadBeforeTimerArmed$'` passes with
  0 failures (8.1 s).
- `go test -race -count=1000 -run
  '^TestIdentityEpochCacheWaiterWaitBudgetIsCumulative$'` passes with
  0 failures (8.9 s).
- `go test -race -count=1 -run '^TestIdentityEpochCacheWaiter'` (the whole
  waiter set) passes.

The change moves one `clockNow()` call earlier. It adds no call, no
allocation, no lock, and no batching or query change. The accounted wait grows
by the sub-microsecond timer-arm interval, far below the 30 s heartbeat
budget, and the bound is now exact instead of overstated.

No-Observability-Change: no metric, span, log line, or status field is added
or renamed. An operator sees waiter parks bounded at exactly one heartbeat
interval instead of slightly over; every existing
`eshu_dp_identity_cache_flight_waiter_total` outcome keeps its meaning.
