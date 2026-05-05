# AGENTS.md — internal/queue guidance for LLM assistants

## Read first

1. `go/internal/queue/README.md` — state machine diagram, transitions, exported
   surface, and invariants
2. `go/internal/queue/models.go` — `WorkItem`, `WorkItemStatus`, `RetryState`,
   `FailureRecord`, and all transition methods
3. `go/internal/queue/doc.go` — package contract statement
4. `go/internal/storage/postgres/` — the Postgres adapter that persists
   `WorkItem` rows and owns claim/lease/visibility logic

## Invariants this package enforces

- **Value semantics** — every transition method (`Claim`, `StartRunning`,
  `Retry`, `Fail`, `Succeed`, `Replay`) clones the receiver. The original
  `WorkItem` is never mutated. Callers must persist the returned value.
- **Claimable status gate** — only `StatusPending` and `StatusRetrying` are
  claimable. `Claim` enforces this; all other statuses return an error.
- **Retry timing** — `Retry` rejects a `nextAttempt` before `now`. The
  Postgres adapter is responsible for choosing a sensible back-off duration;
  this package enforces only that the value is non-negative.
- **Replay budget reset** — `Replay` sets `AttemptCount` to zero. A replayed
  item restarts its retry budget from scratch.
- **StatusFailed is legacy** — new terminal failures must go through `Fail`,
  which writes `StatusDeadLetter`. `StatusFailed` exists only so old rows can
  reach `StatusPending` via `Replay`.

## Common changes and how to scope them

- **Add a new field to `WorkItem`** → add it with a zero value; ensure `clone`
  copies it for pointer fields; update `models_test.go` to cover the new field
  in round-trip assertions; confirm the Postgres schema column is added in
  `internal/storage/postgres` in the same PR.

- **Add a new status** → add the constant to `models.go`; update
  `canTransitionFromClaimable` if it should be claimable; add a test case in
  `models_test.go`; update the state machine diagram in `README.md`.

- **Change retry back-off** → the back-off duration is computed by the caller
  (projector/reducer worker), not in this package. Change the back-off in the
  relevant worker, not here.

## Failure modes and how to debug

- Symptom: `Claim` returns "cannot claim work item in status X" → item is in a
  non-claimable state; check whether a previous worker left the item in
  `StatusRunning` without acking or failing (lease expiry in the Postgres
  adapter should eventually return it to claimable).

- Symptom: `Retry` returns "next attempt cannot be before now" → the caller is
  passing a `nextAttempt` timestamp computed from a stale clock; check that the
  caller uses the same clock source as `now`.

- Symptom: operator sees a work item stuck in `StatusClaimed` forever →
  the worker holding the lease died without calling `StartRunning` or `Fail`;
  the Postgres adapter's lease-expiry scan should recover it; check
  `eshu_dp_queue_oldest_age_seconds` and the adapter's claim-expiry logic.

## Anti-patterns specific to this package

- **Adding I/O** — this is a pure value package. No database connections,
  HTTP clients, or global variables belong here.

- **Mutating `WorkItem` in place** — always use the value returned by
  transition methods. In-place mutation bypasses the clone invariant and
  produces silent aliasing bugs.

- **Producing `StatusFailed` rows** — use `Fail` for terminal failures; it
  writes `StatusDeadLetter`. `StatusFailed` is a read path for legacy rows
  only.

## What NOT to change without an ADR

- `WorkItemStatus` string values — these are stored on disk. Changing a value
  string without a migration corrupts legacy rows.
- Transition guard logic in `canTransitionFromClaimable` — the projector and
  reducer rely on this gate to determine which rows their claim queries may
  return.
