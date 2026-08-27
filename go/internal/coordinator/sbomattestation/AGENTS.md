# AGENTS.md - internal/coordinator/sbomattestation guidance

## Read first

1. `README.md` for ownership and invariants.
2. `planner.go` for validation and deterministic identities.
3. `../sbom_attestation_service.go` for root scheduling and admission.
4. `../plannercontract/README.md` for plan-key grammar.

## Invariants

- Keep provider calls, artifact reads, and credential resolution out.
- Keep all methods on `coordinator.Service` in the parent package.
- Do not import the parent coordinator package.
- Preserve IDs, requested-scope shape, target order, and fairness keys.

## Common changes

Write a failing planner test before changing target parsing or identities.
Scheduling order, admission, and telemetry changes belong in the parent.

## Failure modes

Invalid configuration, duplicate scopes, disabled instances, and unsafe plan
keys fail before a workflow row is returned.

## Verification

Run child tests, recursive coordinator tests, and whole-module build and vet.
