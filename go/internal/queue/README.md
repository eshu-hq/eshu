# Queue

## Purpose

`queue` defines the durable Go work-item contracts shared by projector,
reducer, and replay paths.

## Ownership boundary

This package owns the storage-neutral work-item value, lifecycle status enum,
transition methods, retry state, and failure record carriers. Postgres-backed
claiming, leases, visibility scans, and persistence live in
`internal/storage/postgres`.

## Exported surface

Use `doc.go` and `go doc ./internal/queue` for the full contract. The main
surface is `WorkItem` plus `Claim`, `StartRunning`, `Retry`, `Fail`, `Succeed`,
`Replay`, `ScopeGenerationKey`, `RetryState`, and `FailureRecord`.

## Dependencies

`queue` imports only the Go standard library.

## Telemetry

This package emits no metrics, spans, or logs. Storage adapters and consumer
workers record telemetry around claims, transitions, retries, and processing.

## Gotchas / invariants

- Transition methods clone the receiver and return the updated value. Persist
  the return value; the original is not mutated.
- `Claim` accepts only pending or retrying items and requires a positive lease.
- `Retry` requires `nextAttempt` to be at or after `now`.
- `Fail` writes `dead_letter`; do not produce new `failed` rows.
- `StatusFailed` remains only so legacy rows can be replayed.
- `Replay` resets the retry budget to zero.
- `StartRunning`, `Succeed`, and `Fail` clear stale visibility timestamps.

## Related docs

- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `go/internal/storage/postgres/README.md`
