# Evidence: fired-timer test seam waits on the buffered outcome (#11239)

Records the root-cause, performance and observability evidence for the fix to
the intermittent `TestExecuteHandlerFiredTimerAttributesFromBoundNotTimer`
failure in `go/internal/reducer`.

## Root-Cause Evidence:

CI job 137204381115 (run 45021128157, go-race) failed with
`run 193: abandoned = true, want false: the outcome was already buffered` in
the `parent deadline before the bound` case.

The test seam (`firedTimerService`) waited for the executor to return, built a
zero-duration timer, slept 3 ms, then handed the timer to `executeHandler`. The
sleep assumed the handler goroutine would send its outcome on the `done`
channel within 3 ms of `Execute` returning. When that goroutine was
descheduled for longer, `executeHandler` saw only the timer ready, its
non-blocking recheck of `done` found nothing, and it took the abandon branch.

Observation: a throwaway shim that stalls the handler goroutine 5 ms after the
seam's `returned` signal (the window a preemption or GC pause opens) produced
`abandoned=true` with the CI error text on 3 of 3 runs on the base commit.
Natural reproduction did not occur locally: 0 failures in 30 counts at host
load ~48 on 16 cores, and 0 in 5 counts with 8 in-process busy loops at
`GOMAXPROCS=4` (1,200 handler calls per count). The CI failure was one call in
a run of 400.

The product select is not the cause. When the timer arm fires and no outcome
is buffered, the handler goroutine has not sent yet, which `executeHandler`
cannot tell apart from a handler still running at the deadline. It already
prefers a buffered outcome through the non-blocking recheck. In production the
timer for a parent deadline earlier than the bound is armed at the full bound,
so this seam's instant-fire timer is a test-only shape.

## Change shape

`executeHandler` now sends each outcome through `Service.bufferOutcome`, which
performs the same one-slot channel send and then calls the unexported
`outcomeBuffered` test seam when it is set. The fired-timer seam waits on that
signal, not on `Execute` returning plus a sleep, so both select arms are ready
by construction. The settle sleep and `returnSignalExecutor` are removed. No
worker count, queue, lease, retry budget, batch size, claim path or ack path
changes. Assertions are unchanged.

## No-Regression Evidence:

- Same 5 ms stall probe against the new seam, `-race`: 0 abandonments. It ran
  in every soak below.
- Soak under 8 in-process busy loops at `GOMAXPROCS=4`, `-race`: 40 counts
  (48,000 `executeHandler` calls) and 41 stall probes, 0 failures. Each count
  took about 15 s under that load, so the run stopped at the 10-minute
  `go test` timeout. It did not hang: both subtests were still making
  progress at the timeout.
- Soak at ambient host load (about 42 on 16 cores), `-race`, `-count=2000`
  with a 30-minute timeout: 1,784 counts completed (2,140,800
  `executeHandler` calls) and 1,785 stall probes, 0 failures. The timeout then
  stopped the run short of 2,000 counts.
- Production mutations still fail the test. Removing the timer arm's
  buffered-outcome recheck fails on run 0 with
  `abandoned = true, want false`. Restoring the pre-#11193 relabeling fails
  with `timeout class on 191/400 runs, want 0/400`, which also shows that both
  select arms are really ready and Go picks between them about half the time.

## Performance Evidence:

A throwaway benchmark called `executeHandler` once per op with an executor
that returns immediately: `-cpu 4`, `-count=6`, Go 1.26, no backend in this
path. Bounded (1 h bound): base 3,128 to 3,195 ns/op, head 3,129 to
3,206 ns/op, 1,576 B/op and 12 allocs/op on both. Unbounded: base 1,355 to
1,429 ns/op, head 1,208 to 1,466 ns/op, 1,056 B/op and 5 allocs/op on both.
The added nil check is within noise and adds no allocation.

## No-Observability-Change:

No metric, span, log field or failure class is added, renamed or changed. The
fix touches a test seam and a nil-checked hook. The abandon and timeout paths
an operator sees (`eshu_dp_reducer_handler_abandoned_total`,
`eshu_dp_reducer_handler_timed_out_total`, the `reducer_handler_timeout`
class) behave exactly as before.
