# AGENTS.md — internal/reducer/maintenance/infra

Scoped instructions for this leaf. The root `AGENTS.md` and the maintenance
parent `AGENTS.md` still apply; these add to them.

## Import rule

This leaf may import `internal/telemetry` and `pkg/log`. It must
**never** import the parent `internal/reducer` package, directly or
transitively.

## What must stay conservative

- A repository that differs is repaired ONLY when the next cycle finds
  it still differing. A single-check repair would corrupt a write
  caught between its content commit and its derive.
- `Wrapped` is true only when the cycle's walk ran and reached the end
  of the repository list; a cycle whose budget went to fence marks
  and suspects runs no walk and does not wrap.
- `contextDone` is intentionally duplicated from the `retention` leaf;
  keep both copies in lockstep or hoist deliberately, never by
  importing one leaf from the other.

## Gates that will fire on your change

- **`verify-telemetry-coverage.sh`** — `runner.go` needs its row in
  `docs/public/observability/telemetry-coverage.md`, keyed by file path.
- **`verify-dirgate.sh`** — no ledger row to re-derive. A move or shrink needs no
  dirgate action; growth of a directory past max(40, its count at the base) needs a
  `Dirgate-Growth:` trailer. Never edit the write-once baselines.

## Do not

- Do not name this leaf `inventory`: `storage/postgres/infra/inventory`
  already owns that package name in the wiring files this leaf's
  consumers share.
- Do not move a `Service`-level "starts side runner" wiring test into
  this leaf. `Service` is root-owned; that proof stays in
  `internal/reducer` beside the other `TestServiceStarts*` tests
  (`infra_inventory_reconcile_runner_service_test.go`).
