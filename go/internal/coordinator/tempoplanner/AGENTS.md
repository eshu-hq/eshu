# AGENTS.md - internal/coordinator/tempoplanner guidance

## Read first

1. `README.md` for ownership and invariants.
2. `planner.go` for validation and deterministic identities.
3. `../tempo_service.go` for root scheduling and admission.
4. `../plannercontract/README.md` for plan-key grammar.

## Invariants

- Keep Tempo calls and credential resolution out.
- Keep all methods on `coordinator.Service` in the parent package.
- Do not import the parent coordinator package.
- Preserve exact validation errors, IDs, configured work-item order, sorted
  requested-scope order, and per-target fairness keys.
- Validate duplicate scopes before disabled-target and requested-scope
  filtering.
- Never persist token environment names in requested-scope metadata.

## Common changes

Write a failing planner test before changing request validation, target parsing,
filtering, ordering, or identities. Scheduling order, tenant and egress
filtering, admission, retries, and telemetry changes belong in the parent.

## Failure modes

Invalid configuration, missing or duplicate target scopes, disabled instances,
claims-disabled instances, zero observation times, unsafe plan keys, and invalid
trigger overrides fail before a workflow row is returned.

## Verification

Run child tests, recursive coordinator tests, scoped race, package-doc and
dirgate checks, and whole-module build and vet.
