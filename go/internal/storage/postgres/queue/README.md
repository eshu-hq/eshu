# Postgres queue failure/backoff helpers

## Purpose

This package holds the failure-classification and retry-backoff helpers
shared by the Postgres projector and reducer work queues. It is the
`queue/` leaf of the storage/postgres split (#6693) and, for now, holds only
the two pure helper files the split hoisted out of the parent package. The
queue types themselves (`ProjectorQueue`, `ReducerQueue`) stay in the parent
`postgres` package until their own later `queue/projector/` and
`queue/reducer/` moves.

## Ownership boundary

This package owns failure-cause reconciliation (`QueueFailureMetadata`,
`DeadLetterTriageMetadata`) and retry-delay computation
(`ComputeRetryDelay`, `DefaultRetryMaxDelayFallback`,
`DefaultJitterSource`). The parent `postgres` package owns the queue types
that call these helpers, the claim/fail/ack SQL, and the lease fencing.
`internal/projector/failure` owns the operator-facing triage classification
this package's dead-letter path defers to.

## Exported surface

- `QueueFailureMetadata(cause error, fallbackClass string) (string, string, string)`
- `DeadLetterTriageMetadata(cause error, stage string, retryable bool) (string, string, string)`
- `ComputeRetryDelay(baseDelay, maxDelay time.Duration, jitterFraction float64, attempt int, jitterSource func() float64) time.Duration`
- `DefaultRetryMaxDelayFallback time.Duration` (const)
- `DefaultJitterSource() float64`

See `doc.go` for the godoc contract.

## Dependencies

- `internal/projector/failure` for `TriageFailure`.
- `math/rand/v2` for jitter.

## Telemetry

None. Every function here is a pure computation; the queue types that call
them own the retry-surge counters and failure-class attributes.

## Gotchas / invariants

- `QueueFailureMetadata` and `DeadLetterTriageMetadata` never change a
  self-classifying error's own class or details; they only supply a
  fallback. Changing that precedence changes which failure_class lands on
  retrying and dead-lettered rows.
- `ComputeRetryDelay`'s doubling loop is the actual overflow guard for a
  large attempt count against a multi-minute base delay; `maxBackoffShift`
  only bounds loop iterations. Do not "simplify" the loop to
  `baseDelay*(1<<attempt)` — that overflows `time.Duration` and wraps
  negative.
- Do not import the parent `postgres` package: that is an import cycle.

No-Observability-Change: this move relocates `queueFailureMetadata`,
`deadLetterTriageMetadata`, `computeRetryDelay`, `defaultRetryMaxDelayFallback`,
and `defaultJitterSource` (exporting each) plus the `sanitizeFailureText`
helper they depend on, byte-identical in logic. No metric, span, or log
name changes; the root queue types keep emitting the same
`ProjectorRetrySurge`/`ReducerRetrySurge` counters around the calls.

No-Regression Evidence: `go test ./internal/storage/postgres/... -race -count=1`
passes on the moved tree, exercising the retry-surge, dead-letter, and
backoff-cap paths through the root queue types that now call this package.

## Related docs

- [Postgres storage](../README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
