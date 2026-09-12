# Coordinator CI/CD namespace agent guide

## Read first

1. `README.md` for this documentation-only namespace boundary.
2. `../AGENTS.md` for coordinator scheduling and runtime invariants.
3. The destination leaf's `AGENTS.md`, `README.md`, and source before changing
   a CI/CD planning contract.

## Invariants

- Keep this package documentation-only.
- Keep planner implementation in leaf packages and runtime behavior in the
  coordinator root.
- Leaves must not import the coordinator root.
- Scheduling, clocks, gates, durable admission, retries, queues, leases, and
  telemetry remain in the coordinator root.

## Common changes

Move or add a CI/CD planner only with a production-and-test import edge census.
Update all consumers in the same change; do not leave forwarding packages or
duplicate contracts.

## Verification

Run the changed leaf tests, every direct consumer, the package-documentation
gate, dirgate, and the moved-file-reference guard.
