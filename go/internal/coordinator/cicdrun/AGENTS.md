# AGENTS.md - internal/coordinator/cicdrun guidance

## Read first

1. `go/internal/coordinator/cicdrun/README.md` for this planner's ownership
   boundary and invariants.
2. `go/internal/coordinator/cicdrun/planner.go` for request validation, target
   parsing, and deterministic workflow identities.
3. `go/internal/coordinator/cicd_run_service.go` for the root planner interface,
   scheduling position, plan-key construction, and durable admission call.
4. `go/internal/coordinator/plannercontract/README.md` for the shared plan-key
   grammar.
5. `go/internal/workflow/cicd_run_config.go` for the collector configuration
   validation contract.

## Invariants

- Keep provider calls and credential resolution out of this package.
- Keep `cicd_run_service.go` and all methods on `coordinator.Service` in the
  parent package.
- Do not import `internal/coordinator`; the parent imports this child to name
  `PlanRequest` in its structural planner interface.
- Keep deterministic identities stable for the same instance, plan key, and
  target scope.
- Keep credential environment names out of durable requested-scope metadata.

## Common changes

- Target parsing or identity changes require a failing planner test first and
  focused tests for every changed target shape.
- Shared plan-key changes belong in `plannercontract`, not in a local copy.
- Interface or scheduling-order changes belong in the parent coordinator
  package and need parent service tests.

## Failure modes

- A duplicate `scope_id` fails planning before any work item is returned.
- A blank, path-like, or unsupported plan key fails through
  `plannercontract.ValidateSafePlanKey`.
- Invalid or disabled collector configuration fails through the workflow
  configuration contract.

## Verification

Run focused child tests, then the recursive coordinator tree. Build and vet the
whole module because `cmd/workflow-coordinator` owns the concrete wiring.
