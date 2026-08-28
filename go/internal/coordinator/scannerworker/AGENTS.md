# AGENTS.md - internal/coordinator/scannerworker guidance

## Read first

1. `README.md` for ownership and invariants.
2. `planner.go` for validation and deterministic identities.
3. `../service_scanner_worker.go` for root scheduling and admission.
4. `../plannercontract/README.md` for plan-key grammar.

## Invariants

- Keep source reads, artifact access, and credential resolution out.
- Keep all methods on `coordinator.Service` in the parent package.
- Do not import the parent coordinator package.
- Preserve requested-scope privacy, configured target order, IDs, and fairness
  keys.

## Common changes

Write a failing planner test before changing target parsing or identities.
Scheduling order, admission, retries, and telemetry changes belong in the
parent.

## Failure modes

Invalid configuration, duplicate scopes, disabled instances, claim-disabled
instances, and unsafe plan keys fail before a workflow row is returned.

## Verification

Run child, parent coordinator, and workflow-coordinator command tests.
