# AGENTS.md — internal/reducer/maintenance/obligation

Scoped instructions for this leaf. The root `AGENTS.md` and the maintenance
parent `AGENTS.md` still apply; these add to them.

## Import rule

This leaf may import `internal/telemetry` and `pkg/log`. It must
**never** import the parent `internal/reducer` package, directly or
transitively. The `Store`/`Maintainer` ports are the only seams to
storage; implementations live in `storage/postgres`.

## What must stay conservative

- `Runner` MUST call the maintenance port only after a Finalize returned
  `phase_not_ready`, MUST NOT finalize again after a failed callback,
  and MUST keep each cycle bounded by `MaxPerCycle`. Whole-corpus
  deferred maintenance is a test control arm only; never wire it as the
  shipped `Maintainer` (#7584).
- On `ErrInapplicable` the runner MUST retire the row through
  `RetireActivationInapplicable` and MUST NOT count a maintenance
  failure. On `ErrCatalogChanged` it MUST keep the lease, MUST NOT
  finalize again or run any fallback pass, and MUST count
  `failures_total{reason="catalog_changed"}` with an Info log (#7584).
- The maintenance callback MUST stay bounded by the obligation's lease
  (`maintenanceContext`): cancel a margin before the lease ends so a
  slow pass cannot run concurrently with the next owner.
- Method names on the ports (`ClaimActivation`, `FinalizeActivation`,
  `ActivationStats`, ...) are the storage contract; implementations in
  `storage/postgres/activation` must match them exactly.

## Gates that will fire on your change

- **`verify-telemetry-coverage.sh`** — `runner.go`, `settle.go`, and
  `errors.go` need rows in
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
