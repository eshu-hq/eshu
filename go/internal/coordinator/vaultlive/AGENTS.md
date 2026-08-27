# AGENTS.md - internal/coordinator/vaultlive guidance

## Read first

1. `README.md` for ownership and invariants.
2. `planner.go` for validation and deterministic identities.
3. `../vault_live_service.go` for root scheduling and admission.
4. `../plannercontract/README.md` for plan-key grammar.

## Invariants

- Keep Vault calls and credential resolution out.
- Keep all methods on `coordinator.Service` in the parent package.
- Do not import the parent coordinator package.
- Preserve IDs, requested-scope shape, configured work-item order, sorted
  requested-scope order, and per-target fairness keys.
- Never persist Vault addresses, token environment names, cluster IDs, or
  namespaces in planner identities or requested-scope metadata.

## Common changes

Write a failing planner test before changing target parsing or identities.
Scheduling order, admission, retries, and telemetry changes belong in the
parent.

## Failure modes

Invalid configuration, duplicate targets, disabled instances, claims-disabled
instances, zero observation times, and unsafe plan keys fail before a workflow
row is returned.

## Verification

Run child tests, recursive coordinator tests, scoped race, and whole-module
build and vet.
