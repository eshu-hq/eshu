# AGENTS.md — internal/reducer/maintenance/producer

Scoped instructions for this leaf. The root `AGENTS.md` and the maintenance
parent `AGENTS.md` still apply; these add to them.

## Import rule

This leaf may import `internal/telemetry` and `pkg/log`. It must
**never** import the parent `internal/reducer` package, directly or
transitively. The `Store` port is the only seam to storage; its
implementation lives in `storage/postgres`.

## What must stay conservative

- `Runner` MUST keep each cycle bounded by `MaxPerCycle`; housekeeping
  (one bounded prune, the census gauges) runs once per cycle on one
  worker.
- There MUST be no catch-up: a generation whose consumers already
  replayed is indistinguishable from one that never did, so a
  catch-up would re-owe every pruned generation (#7635).
- On `ErrSettleLockTimeout` the runner MUST count
  `failures_total{reason="settle_lock_timeout"}` at Warn with no span
  error; nothing was written and the next claimer settles it.
- Method names on the port (`ClaimProducerActivation`,
  `SettleProducerActivation`, `ProducerActivationStats`, ...) are the
  storage contract; implementations in `storage/postgres` must match
  them exactly.

## Gates that will fire on your change

- **`verify-telemetry-coverage.sh`** — `runner.go` needs its row in
  `docs/public/observability/telemetry-coverage.md`, keyed by file path.
- **`verify-performance-evidence.sh`** — fires on this path (worker,
  queue, lease). Markers must be unbolded and line-initial in a tracked
  note.
- **`verify-dirgate.sh`** — re-derive the `internal/reducer` row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the
  mirror; never hand-edit either.

## Do not

- Do not move a `Service`-level "starts side runner" wiring test into
  this leaf. `Service` is root-owned; that proof stays in
  `internal/reducer` beside the other `TestServiceStarts*` tests.
- Do not reduce worker concurrency or batch sizes to fix a settle race;
  make the write idempotent under concurrent execution instead.
