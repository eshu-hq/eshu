# AWS projector namespace agent guide

## Read first

1. `README.md` for this namespace boundary.
2. `../AGENTS.md` for projector-wide lifecycle and fan-out invariants.
3. The destination leaf's `AGENTS.md`, `README.md`, and `doc.go` before
   changing an AWS intent builder.

## Invariants

- This package is documentation-only. Keep intent builders in leaf packages
  and orchestration in `internal/projector`.
- Leaves import the neutral `internal/projector/intent` contract, never the
  parent projector package.
- Preserve root-owned lookup construction, fan-out order, queue writes,
  retries, and telemetry.
- Preserve trigger facts, entity keys, reasons, domains, and source-system
  derivation when moving a leaf.

## Verification

Run the changed leaf tests, root projector tests, package-documentation and
dirgate checks, telemetry coverage, and the golden gates selected by the
changed paths. The orchestrator owns the final `make pre-pr` run.
