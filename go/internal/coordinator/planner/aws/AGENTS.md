# AWS planner namespace agent guide

## Read first

1. `README.md` for the split between freshness and scheduled planning.
2. `../AGENTS.md` for planner-wide ownership rules.
3. `freshness/AGENTS.md` or `scheduled/AGENTS.md` for the leaf being changed.

## Invariants

- Keep the freshness and scheduled planner leaves separate.
- Preserve the one-way dependency `scheduled` to `freshness`.
- Keep AWS API calls, credential resolution, trigger claims, admission,
  retries, and telemetry outside this namespace package.
- Do not copy target-scope parsing between leaves. Keep the scheduled
  global-service region policy separate from freshness-trigger authorization.

## Verification

Run both AWS planner packages with the coordinator root when their shared
target-scope contract moves. Retain the AWS binding tests that populate the
scanner registry.
