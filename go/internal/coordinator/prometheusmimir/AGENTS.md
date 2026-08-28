# AGENTS.md - internal/coordinator/prometheusmimir guidance

## Read first

1. `README.md` for ownership and invariants.
2. `planner.go` for validation and deterministic identities.
3. `../prometheus_mimir_service.go` for root scheduling and admission.
4. `../plannercontract/README.md` for plan-key grammar.

## Invariants

- Keep provider calls and credential resolution out.
- Keep all methods on `coordinator.Service` in the parent package.
- Do not import the parent coordinator package.
- Preserve exact validation errors, IDs, configured work-item order, sorted
  requested-scope order, and per-target fairness keys.
- Drop disabled targets before validating blank or duplicate enabled scopes;
  validate enabled scopes before applying the requested-scope filter.
- Never persist provider URLs, token or tenant environment names, or limits in
  requested-scope metadata.
- Return the populated run even when filtering produces no work items; the
  parent owns the no-admission decision for an empty item slice.

## Common changes

Write a failing planner test before changing request validation, target parsing,
filtering, ordering, privacy, or identities. Scheduling order, tenant and egress
filtering, admission, retries, and telemetry changes belong in the parent.

## Failure modes

Invalid instances, zero observation times, unsafe plan keys, invalid trigger
overrides, malformed configuration, and blank or duplicate enabled scopes fail
before workflow rows are returned. Disabled invalid targets are ignored.

## Verification

Run child tests, recursive coordinator tests, scoped race, package-doc and
dirgate checks, and whole-module build and vet.
