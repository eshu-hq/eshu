# #8636 post-receive drop test — performance and observability evidence

No-Regression Evidence: deterministic Site-B test
`TestServiceBatchPostReceiveDropRecordsOnce` plus its ordering seam
(`batchWorkerTestHooks.afterWorkReceive`, called once per worker handoff
receive behind a single nil check) in `go/internal/reducer`
(`service_batch.go` worker loop and hooks type, `service.go` Service struct,
`service_batch_postreceive_test.go`).

- Baseline: with the Site-B record hunk removed the new test FAILS (the
  received item goes unrecorded — the exact false-green class from #8625
  finding F2); with the hunk present it PASSES.
- After: new test `-race -count=50` green; whole `go/internal/reducer`
  package green (`-count=1`); `go vet` and `gofmt` clean.
- Backend/version: in-process Go test only (`go1.26.9 linux/arm64`); no
  Neo4j/Postgres backend involved — the seam and test exercise the
  shutdown-path worker loop.
- Input shape: one staged intent, two workers, hook cancels the run context
  between the handoff rendezvous receive and the post-receive check.
- Terminal counts: exactly one `ack_outcome_unknown` record carrying a real
  worker ID (never `-1`); zero executions, zero acks, zero fails.
- Why safe: production wiring never sets the hook, so every call site is a
  single nil check and the hot path (claim → handoff → execute) is
  instruction-identical with the hook absent. No worker count, batch size,
  poll interval, lease, or queue change. No benchmark delta to measure.

No-Observability-Change: the seam adds no metric instrument, metric label,
span, log key, route, graph query shape, queue table, worker, lease, or
runtime knob. It adds no records of its own — the asserted
`ack_outcome_unknown` line is the pre-existing Site-B record from #8625.
Operators keep diagnosing the path through the existing reducer run spans,
execution counters, and Postgres query instrumentation.
