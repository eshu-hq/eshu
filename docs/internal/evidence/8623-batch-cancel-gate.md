# #8623 batch-claim cancel gate — performance and observability evidence

No-Regression Evidence: post-`ClaimBatch` `ctx.Err()` gate in
`go/internal/reducer/service_batch.go:134-152`.

- Baseline: `TestServiceBatchCanceledBeforeEnqueueHasNoSuccess` flaked 3/3
  CI occurrences (`Fail calls = 1`); local `-race -count=200` green (window
  too narrow to catch naturally).
- After: fixed test `-race -count=50` green (lane) and `-race -count=10`
  green (orchestrator); whole `go/internal/reducer` package green;
  `Batch*` tests `-race -count=5` green (26.3 s).
- Why safe: one `ctx.Err()` check per non-empty batch (nanoseconds, off the
  claim path); zero behavior change when ctx is live; the gate only runs at
  shutdown, where the alternative was execute-then-abandon. No worker count,
  batch size, poll interval, lease, or queue change. No benchmark delta to
  measure: the hot path (claim → handoff → execute) is instruction-identical
  when the run context is live.

No-Observability-Change: dropped-after-cancel batches now emit one
`ack_outcome_unknown` line per item (counter + warn log + duration and
queue-wait histograms) through the pre-existing `recordBatchAckOutcome`
path; claim telemetry (`QueueClaimDuration`, `BatchClaimSize`) is recorded
before the gate and unchanged. No new instruments, no new log shapes
beyond the existing unknown-outcome line with `workerID: -1` (never
assigned to a worker; dropped between claim and handoff).
