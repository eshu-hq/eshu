# AGENTS.md — internal/recovery guidance for LLM assistants

## Read first

1. `go/internal/recovery/README.md` — purpose, exported surface, and
   invariants
2. `go/internal/recovery/replay.go` — `Handler`, `ReplayStore`, `ReplayFilter`,
   `RefinalizeFilter`, `Stage` constants
3. `go/internal/recovery/doc.go` — package contract statement
4. `go/internal/storage/postgres/` — `ReplayStore` implementation
5. `CLAUDE.md` section "Facts-First Bootstrap Ordering" — why replay must
   respect phase ordering

## Invariants this package enforces

- **Stage validation** — `ReplayFilter.Stage` must be `StageProjector` or
  `StageReducer`. Any other value causes `Validate` to return an error before
  the store is called.
- **Refinalize is always scoped** — `RefinalizeFilter.ScopeIDs` must be
  non-empty. Unbounded refinalize is not supported; the filter rejects an empty
  slice.
- **UTC clock** — `Handler.time()` returns `time.Now().UTC()`. Test stubs that
  inject `now` via the unexported `now` field must use UTC values to match
  assertions.
- **No direct graph mutation** — recovery is queue replay only. After a replay,
  the projector or reducer re-runs the full write pipeline.

## Common changes and how to scope them

- **Add a new recovery operation** → define a new filter and result type in
  `replay.go`; add a `Validate` method on the filter; add the corresponding
  method on `ReplayStore`; implement it in `internal/storage/postgres`; add a
  method on `Handler`; add tests in `replay_test.go`.

- **Add a new `Stage` constant** → add the constant to `replay.go`; update the
  `switch` in `ReplayFilter.Validate`; add a test case. Ensure the new stage
  string matches the queue table's `stage` column values.

- **Add filtering by failure class** → the `FailureClass` field already exists
  in `ReplayFilter`; check whether the `ReplayStore` implementation in
  `internal/storage/postgres` passes it through to the SQL query.

## Failure modes and how to debug

- Symptom: `ReplayFailed` returns "replay filter requires a valid stage" →
  caller passed an empty or unrecognized `Stage`; check the admin handler
  parameter parsing.

- Symptom: `Refinalize` returns "refinalize filter requires at least one
  scope_id" → caller passed an empty `ScopeIDs` slice; the admin handler must
  validate that at least one scope ID was supplied.

- Symptom: replayed items re-enter `dead_letter` immediately → the failure is
  not transient; check the `FailureClass` field in the work item's failure
  record before replaying; `projection_bug` and `input_invalid` classes
  indicate code or data problems that replay will not fix.

- Symptom: refinalized scopes do not produce new graph nodes → the projector
  queue has new pending rows but workers are not draining them; check
  `eshu_dp_queue_oldest_age_seconds{queue="projector"}` and projector worker
  health.

## Anti-patterns specific to this package

- **Direct graph or Postgres writes** — all mutations go through `ReplayStore`.
  This package must not open connections or run SQL directly.

- **Unbounded refinalize** — always pass explicit `ScopeIDs`. A missing
  filter guard on the caller side will be caught by `RefinalizeFilter.Validate`,
  but add the guard at the call site too for clarity.

- **Skipping filter validation** — always call `Validate` (or rely on
  `Handler.ReplayFailed` / `Handler.Refinalize` to do so) before passing a
  filter to the store. The store implementation may not re-validate.

## What NOT to change without an ADR

- `StageProjector` and `StageReducer` string values — these map to queue table
  `stage` column values stored on disk; changing them without a migration
  breaks existing rows.
- `Handler.time()` UTC behavior — callers depend on UTC timestamps matching
  Postgres NOW() UTC comparisons in the store.
