# Coordinator planner namespace agent guide

## Read first

1. `README.md` for the namespace ownership boundary.
2. `../AGENTS.md` for coordinator runtime and scheduling invariants.
3. The destination leaf's `AGENTS.md`, `README.md`, and `planner.go` before
   changing a planner.

## Invariants

- This package is documentation-only. Keep runtime declarations in leaf
  packages and orchestration in `internal/coordinator`.
- Planner leaves must not import the parent coordinator package.
- Preserve root-owned planner interfaces, scheduling order, durable admission,
  retries, queue and lease behavior, and telemetry.
- `contract` owns only the shared safe plan-key grammar. Terraform-state keeps
  its stricter local validator.

## Common changes

Add or move a planner only with an import-edge census covering production and
tests. Update every caller in the same change; do not leave forwarding packages
or duplicate contract types.

## Verification

Run the changed leaf tests, `go test ./internal/coordinator/...` and the package
documentation and dirgate checks. The orchestrator owns the final `make pre-pr`
run.
