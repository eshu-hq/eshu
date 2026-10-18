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
- `RunOnce` gives the pruner a search index pre-delete deadline of a quarter
  of the poll interval when the policy sets none (#7427), so one pass's
  committed pre-delete batches end well before the next poll. Keep the
  configured policy unchanged; copy it per pass.
- `RunOnce` runs the search index orphan sweep (#7427 PR2) only after
  `PruneSupersededGenerations` returns, never inside it, so the sweep never
  holds or waits on a retention scope lock. Keep the poll-interval cadence
  gate (`TestGenerationRetentionRunnerSweepsOrphansOncePerPollInterval`).
- `RunOnce` runs the flagged superseded sweep (#8231) only after the orphan
  sweep, under its own flag (`Policy.SearchIndexSupersededSweepEnabled`, off
  by default) and its own poll-interval cadence gate
  (`TestGenerationRetentionRunnerSweepsSupersededOncePerPollInterval` and the
  off-by-default test). Never enable the flag until the issue's gate-(b)
  dead-tuple/bloat evidence lands.

## Gates that will fire on your change

- **`verify-telemetry-coverage.sh`** — every non-test file here is covered
  by the `generation retention` row in
  `docs/public/observability/telemetry-coverage.md`, keyed by the glob
  `go/internal/reducer/maintenance/retention/*.go`.
- **`verify-performance-evidence.sh`** — fires on this path (batching,
  retention transaction). Markers must be unbolded and line-initial in
  a tracked note.
- **`verify-dirgate.sh`** — no ledger row to re-derive. A move or shrink needs no
  dirgate action; growth of a directory past max(40, its count at the base) needs a
  `Dirgate-Growth:` trailer. Never edit the write-once baselines.

## Do not

- Do not move a `Service`-level "starts side runner" wiring test into
  this leaf. `Service` is root-owned; that proof stays in
  `internal/reducer` beside the other `TestServiceStarts*` tests
  (`generation_retention_runner_service_test.go`).
