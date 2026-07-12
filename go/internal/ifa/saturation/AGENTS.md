# AGENTS.md - internal/ifa/saturation guidance

## Read first

1. `doc.go` and `README.md` here.
2. `go/internal/ifa/AGENTS.md` — the parent Ifá contract-layer invariants.
3. `go/internal/storage/cypher/backpressure_executor.go` — the real
   `BackpressureGate` and `GraphWriteTimeoutError` this Odù exercises; its doc
   comment is the #3560 root-cause statement.
4. `go/internal/storage/postgres/reducer_queue_helpers.go` — the real
   `retryable`/`failIntent` decision this package mirrors, and
   `reducer_queue_readiness_sql.go`'s `nonCountingReducerRetryFailureClasses`
   (which `graph_write_timeout` is deliberately NOT in).

## Invariants

- Exercise the REAL seams: `cypher.NewBackpressureGate`,
  `cypher.GraphWriteTimeoutError`, `reducer.IsRetryable`, and
  `ifa.DeadLetterSetsEqual`. Do not fork a private copy of the retry decision or
  the timeout error — the regression's value is that it tracks production code.
- `graph_write_timeout` is a COUNTING retry class. Do not model it as
  non-counting; the whole point is that oversubscription exhausts the attempt
  budget, which is why an unbounded gate floods.
- Keep the scenario deterministic under `-race`. The ungated flood uses an
  overlap barrier; the gated round pins holders until the surplus blocks. Do not
  reintroduce a wall-clock settle for correctness — timing may only gate the
  engagement observer, never the pass/fail counts.
- This is a hermetic model, not the real queue. Do not add Postgres, a graph
  backend, or network here. Full-scale saturation calibration is operator-gated
  and lives elsewhere.
- Assert the failure SHAPE, not survival: backpressure engaged, retries
  happened, dead-letter set empty, residual drained to zero.
