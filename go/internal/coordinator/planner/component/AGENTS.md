# Component planner namespace agent guide

## Read first

1. `README.md` for the planner and activation-contract boundary.
2. `../AGENTS.md` for planner-wide ownership rules.
3. `extension/AGENTS.md` and
   `../../componentactivation/AGENTS.md` before changing their shared contract.

## Invariants

- Keep this package documentation-only.
- `componentactivation` remains outside the planner subtree and imports no
  coordinator planner package.
- The extension planner consumes that neutral contract without owning it.
- Scheduling, egress policy, audit, durable admission, retries, and telemetry
  remain in the coordinator root.

## Verification

Run the activation package, extension planner, and coordinator root tests
together for changes to their shared contract.
