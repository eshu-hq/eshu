# internal/queue Agent Instructions

These rules are mandatory for this package. Root `AGENTS.md` still owns the
repo-wide proof, performance, concurrency, and skill-routing rules.

## Read First

1. `README.md` and `doc.go`.
2. `models.go` before changing work-item lifecycle behavior.
3. `go/internal/storage/postgres/README.md` and
   `go/internal/storage/postgres/change-guide.md` before changing persistence,
   leases, claims, or replay behavior.

## Local Rules

- Keep this package storage-neutral and standard-library-only.
- Transition methods must clone the receiver and return the updated value.
  Callers must persist the returned value.
- Only `pending` and `retrying` work items are claimable.
- `Claim` requires a positive lease duration.
- `Retry` must reject `nextAttempt` values before `now`.
- `Fail` writes `dead_letter`; do not create new `failed` rows. `failed` is
  legacy replay compatibility only.
- `Replay` resets retry state and returns terminal items to `pending`.
- `StartRunning`, `Succeed`, and `Fail` must clear stale visibility timestamps.

## Change Gates

- Status or transition changes require tests for valid lifecycle paths,
  invalid transitions, retry timing, replay, and dead-letter behavior.
- Persistence-affecting changes require matching Postgres queue tests.
- Concurrency behavior belongs in the storage adapter; do not add database or
  worker ownership to this package.

## Do Not Change Without Owner Review

- Stored status string values.
- Dead-letter and legacy `failed` replay semantics.
- Clone-on-transition behavior.
