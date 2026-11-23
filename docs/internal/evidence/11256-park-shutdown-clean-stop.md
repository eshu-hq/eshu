# #11256 follow-up: a cancelled Fail ends the run cleanly

Refs #11256, #11298.

A review of #11298 found that a SIGTERM landing inside `WorkSink.Fail` made
`Service.Run` return an error and `cmd/reducer` exit 1. `Fail` commits its retry
UPDATE (or its dead-letter UPDATE) and then parks a wait row: the insert, the
rendezvous wait of up to 3 s, the satisfaction check. Once shutdown cancels the
run context any of those statements fails with the wrapped context error.
`executeWithTelemetry` and `executeAndReport` (the batch loop, the production
default) joined that onto the handler error and the run returned it.

## Root-Cause Evidence

On `origin/main` (`f6a3d9e052`), with only the new test file added to a
throwaway worktree:

| Test | Result before the fix |
| --- | --- |
| `TestServiceRunTreatsShutdownDuringFailAsCleanStop`, workers 0 and 3 | `Service.Run after shutdown during a Fail = handler deferred / fail reducer work: park waiting reducer work: context canceled, want nil` |
| `TestServiceBatchRunTreatsShutdownDuringFailAsCleanStop` | the same error |
| `TestServiceShutdownInterruptedFailIsLoggedAsAWarning`, on the first version of the fix (`c3c5fb6507`) | `log is missing "level=WARN"`: the new status fell to the default case and logged `reducer execution succeeded` |

## The change

A `Fail` error that wraps the cancelled run context (`stoppedByShutdown`) is
recorded with the status `fail_interrupted_by_shutdown` (`recordShutdownInterrupt`)
and the run ends with nil, in the sequential, per-item and batch loops. A `Fail`
error with a live context is returned as before. After a cut `Fail` the item is in
one of three states: the park was cut and the item retries on its backstop; the
retry UPDATE was cut and the claim expires for reclaim; or the dead-letter UPDATE
committed while the client saw the cancel and the item is already dead-lettered.

## Proof

| Check | Result |
| --- | --- |
| The three shutdown tests above, plus the claim and ack variants | pass; `-race -count=20` pass |
| `TestServiceRunStillReturnsAFailFailure` (workers 0 and 3), `TestServiceBatchRunStillReturnsAFailFailure` | a `Fail` error with a live context still ends the run |
| `go test -race -count=1 ./internal/reducer` | ok |
| `go test -count=1 ./cmd/reducer` | ok |

No-Regression Evidence: baseline is `origin/main` at `f6a3d9e052`; after is this branch. The change runs
only on error paths: `stoppedByShutdown` is evaluated only after
`WorkSink.Fail` (in `executeWithTelemetry` and `executeAndReport`),
`ackSingleRetryingTransient` (in `ackReducerWork`) or `WorkSource.Claim` returned
a non-nil error. `git diff origin/main...HEAD` on `service.go` and
`service_batch.go` adds one `if stoppedByShutdown(...)` branch inside each
existing `Fail`-error block and replaces the ack guard added in #11298 with a call
to the same helper; no statement on the success path of a claim, a heartbeat, an
execution or an ack changed. The hot-path functions touched are
`Service.executeWithTelemetry`, `Service.executeAndReport` and
`Service.ackReducerWork`. Worker count, batch size, lease duration, retry budget
and queue SQL are unchanged: no setting, query or lock order changed, so the
conflict domain (the queue row of one claimed item) is the one `Fail` already
holds. No micro-benchmark of the success path exists or was run: the claim is
that no success-path code changed, and the diff above is the proof. Backend: no
database is involved in the new tests (hermetic fakes); the earlier live runs of
`TestServiceSurvivesAWideVerdictDeferralLive` and
`TestWideVerdictWaitersConvergeUnderContentionLive` (postgres 18.6,
`GOMAXPROCS=2`, 100 runs each, 200 of 200 parent runs passing) ran the same
claim, execute, `Fail` and ack code before the commit that only added a log case.
Input shape: one claimed intent whose handler returns an error, with a `Fail`
that blocks until the run context is cancelled. Terminal queue and row counts:
none read or written by the tests; for a real stop the item's queue row is
whatever `Fail` had committed (retrying with its backstop `visible_at`, a claim
left to expire, or dead-lettered). The statement is safe because only errors that
wrap the cancelled run context are converted; every other `Fail` error, and every
`Fail` error under a live context, is returned and ends the run exactly as before.

Observability Evidence: an interrupted `Fail` is counted on the existing
`eshu_dp_reducer_executions_total{status="fail_interrupted_by_shutdown"}` (one
new value of the closed `status` label, documented in
`docs/public/reference/telemetry/metrics-reducer-storage.md` next to
`ack_outcome_unknown`) and logged at WARN as
`reducer fail interrupted by shutdown; the item retries on its backstop, its claim
expires, or it is already dead-lettered (the terminal update committed)` with
`status` and `failure_class` set to `fail_interrupted_by_shutdown`, the intent,
domain and scope attributes, and the handler error joined with the cancelled
`Fail` error. At 3 AM an operator searches the reducer log for that message, or
graphs `sum by (domain) (rate(eshu_dp_reducer_executions_total{status="fail_interrupted_by_shutdown"}[5m]))`:
a non-zero value means a stop landed inside a `Fail`; the joined errors say
whether the handler or the cancelled statement came first, and the item's queue
row says which of the three outcomes applies. Before this change such an event
ended the run with `reducer failed` and exit code 1.

## NOT_CHECKED

- A live reproduction of a cancel inside the park: the window is too narrow to hit
  reliably, so the hermetic tests are the deterministic proof.
- The full registry gate set on this branch (the focused gates and checks listed in
  the PR body were run).
- A success-path micro-benchmark (none was run; see the No-Regression Evidence).
