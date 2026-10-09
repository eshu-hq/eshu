# AGENTS.md — internal/reducer/maintenance/retention

Scoped instructions for this leaf. The root `AGENTS.md` and the maintenance
parent `AGENTS.md` still apply; these add to them.

## Import rule

This leaf may import `internal/telemetry` and `pkg/log`. It must
**never** import the parent `internal/reducer` package, directly or
transitively.

## What must stay conservative

- `Runner` MUST retry a pass that pruned nothing but reported skipped
  candidates soon (1m doubling per consecutive skipped-only pass,
  capped at the poll interval) instead of sleeping the full interval
  (#7398). Lock-held candidates stay invisible by design (SKIP
  LOCKED), so a lock-starved pass keeps the full sleep.
- `contextDone` is intentionally duplicated in the `infra` leaf; keep
  both copies in lockstep or hoist deliberately, never by importing
  one leaf from the other.
- The prune transaction stays bounded: scope-row locks block
  concurrent fact inserts for `ScopeLockHold` (#7279).

## Gates that will fire on your change

- **`verify-telemetry-coverage.sh`** — `runner.go` needs its row in
  `docs/public/observability/telemetry-coverage.md`, keyed by file path.
- **`verify-performance-evidence.sh`** — fires on this path (batching,
  retention transaction). Markers must be unbolded and line-initial in
  a tracked note.
- **`verify-dirgate.sh`** — re-derive the `internal/reducer` row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the
  mirror; never hand-edit either.

## Do not

- Do not move a `Service`-level "starts side runner" wiring test into
  this leaf. `Service` is root-owned; that proof stays in
  `internal/reducer` beside the other `TestServiceStarts*` tests
  (`generation_retention_runner_service_test.go`).
