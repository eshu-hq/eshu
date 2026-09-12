# AGENTS.md - internal/coordinator/sbom/attestation guidance

## Read first

1. `go/internal/coordinator/sbom/attestation/README.md` for ownership and
   invariants.
2. `go/internal/coordinator/AGENTS.md` for root scheduling and runtime
   invariants.
3. `go/internal/coordinator/sbom/attestation/planner.go` for validation and
   deterministic identities.
4. `go/internal/coordinator/sbom_attestation_service.go` for root scheduling
   and durable admission.
5. `go/internal/coordinator/planner/contract/README.md` for plan-key grammar.

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

Run focused child tests, then the recursive coordinator tree. Build and vet the
whole module because `cmd/workflow-coordinator` owns the concrete wiring.
