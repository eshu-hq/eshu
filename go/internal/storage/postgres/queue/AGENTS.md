# AGENTS.md — Postgres queue failure/backoff helpers guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for Postgres storage conventions.
3. `failure_metadata.go` and `backoff.go`, with `backoff_test.go`.

## Invariants

- Keep the package clause as `package queuestore`; callers import the
  `storage/postgres/queue` path without an alias.
- Keep every function pure: no database access, no logging, no globals.
- Keep `QueueFailureMetadata`/`DeadLetterTriageMetadata`'s precedence: a
  self-classifying cause (`classifiedFailure`/`detailedFailure`) always
  wins over the fallback class and the triage details.
- Keep `ComputeRetryDelay`'s overflow guard (the doubling loop breaks on
  `doubled < backoff`) — do not replace it with a direct shift.
- Never import the parent `postgres` package from here.

## Common changes

- Changing the dead-letter triage precedence changes the durable
  `failure_class`/`message`/`details` an operator sees on a dead-lettered
  row; trace every caller in the parent package (`ProjectorQueue.Fail`,
  `ReducerQueue.failIntent`) before doing it.
- Changing `ComputeRetryDelay`'s jitter or cap behavior changes
  `visible_at` scheduling for every retrying work item; re-run the
  retry-storm and overflow-guard tests in `backoff_test.go` and the
  readiness-class defer tests in the parent package
  (`reducer_queue_*_readiness_test.go`).

## Failure modes

- Importing the parent `postgres` package creates an import cycle: the
  parent's `ProjectorQueue`/`ReducerQueue` already import this package.
- A regressed overflow guard in `ComputeRetryDelay` can produce a negative
  duration, scheduling `visible_at` in the past instead of respecting
  `maxDelay` (see `TestComputeRetryDelayCapsAtMaxDelayWithoutOverflowingLargeAttemptCounts`).
- Losing the self-classifying-error precedence in
  `DeadLetterTriageMetadata` replaces an author-curated failure class with
  the generic triage class, losing operator-facing diagnostic context.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/... -count=1
go vet ./internal/storage/postgres/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
