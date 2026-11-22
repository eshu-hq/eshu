# Evidence: reducer handler timeout attribution (#11193)

Records the performance, concurrency and observability evidence for the change
to `executeHandler` in `go/internal/reducer` that attributes a handler timeout
from the bound's own deadline instead of from the timer state.

## Change shape

The handler's execution context and the bound timer used to be armed at two
separate instants. A handler that returned between them produced a bare
`context.DeadlineExceeded` with no timeout class. Both are now derived from one
deadline instant, and the handler goroutine records whether that deadline had
passed when `Execute` returned. No worker count, queue, lease, retry budget,
batch size or claim path changes.

## Performance Evidence:

The change adds one monotonic clock read per handler return and removes a
`timer.Stop()` call from the attribution path. Baseline and after both run the
same handler loop with the same input shape (one handler, one 50 ms bound, the
executor returning after the context ends). There is no database or graph
backend in this path, so there is no backend version to cite; the measured
runtime is Go 1.26 with `-race`. Per-item cost is unchanged within noise; the
only measured quantity that moved is correctness under contention, below.

## No-Regression Evidence:

- Original CI test, `-race`, `-count=2000`, four busy loops on the host: 2000
  passed, 0 failed.
- New deterministic return-before-timer test: fails on the old logic with
  `IsRetryable(context deadline exceeded) = false, want true` (the CI text) and
  passes now.
- Fired timer with a buffered outcome, 400 trials per case. Own deadline: 400
  of 400 carry the timeout class before and after. Downstream deadline error:
  208 of 400 relabelled before the second fix, 0 of 400 after. Parent deadline
  earlier than the bound: 213 of 400 relabelled before, 0 of 400 after.
- Focused reducer tests under `-race` for 100 to 300 runs and the full
  `go test ./internal/reducer` package: exit 0.
- Terminal queue behavior: a handler that times out retries under the attempt
  budget with the `reducer_handler_timeout` class. The bare-error path it
  replaces dead-lettered the item on attempt 1 with the `reduce_intent` class.

## Observability Evidence:

`eshu_dp_reducer_handler_timed_out_total` and the `reducer_handler_timeout`
failure class now fire on the previously misattributed path. A test asserts the
counter increments once, the abandoned counter stays 0, and the item fails once
without an ack when a cooperative handler returns before the timer. An operator
sees the change as timed-out handlers counted and retried instead of appearing
as `reduce_intent` dead letters. No-Observability-Change for metric names,
labels and failure classes: none is added or renamed.
