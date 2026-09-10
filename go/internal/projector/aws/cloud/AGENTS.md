# AWS cloud projector namespace agent guide

## Read first

1. `README.md` for this namespace boundary.
2. `../AGENTS.md` for AWS projector invariants.
3. The destination leaf's `AGENTS.md`, `README.md`, and `doc.go` before
   changing a cloud-specific intent builder.

## Invariants

- This package is documentation-only. Keep intent builders in leaf packages.
- Leaves import `internal/projector/intent`, never the parent projector
  package.
- Preserve root-owned fan-out and reducer-owned graph materialization.

## Verification

Run the changed leaf and root projector tests plus package-documentation and
dirgate checks. The orchestrator owns the final `make pre-pr` run.
