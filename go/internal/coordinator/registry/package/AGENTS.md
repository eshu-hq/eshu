# AGENTS.md - internal/coordinator/registry/package guidance

## Read first

1. `go/internal/coordinator/registry/package/README.md` for ownership
   boundary, the package-name gotcha, and invariants.
2. `go/internal/coordinator/registry/package/scheduler.go` for planner
   request validation and run/work-item construction.
3. `go/internal/coordinator/registry/package/derivation.go` and
   `ecosystem_targets.go` for owned-package derivation and per-ecosystem
   identity construction.
4. `go/internal/coordinator/package_registry_service.go` in the parent
   package for scheduling position and the aliased import.
5. `go/internal/coordinator/schedule/doc.go` for the shared target-class
   ranking and skip-evidence contract.

## Invariants

- Keep the package clause `packages`; do not rename it to match the
  directory even though every other coordinator subpackage matches its
  directory name.
- Always import this package with an explicit alias; `goimports` cannot infer
  one because the directory name (`package`) does not match the package
  clause (`packages`).
- Keep registry and network calls out of this package; it only plans, never
  queries a registry.
- Keep `package_registry_service.go` and all methods on `coordinator.Service`
  in the parent package.
- Do not import `internal/coordinator`; the parent imports this child to name
  `PlanRequest` in its planner wiring.
- Keep derived-target `scope_id` and work-item identities stable for the same
  instance, plan key, and target set.

## Common changes

- A new supported ecosystem needs a new case in
  `packageRegistryEcosystemSpec` and `packageRegistryOwnedDependencyEcosystem`,
  plus a focused derivation test.
- Shared plan-key or target-class-ranking changes belong in `schedule` or
  `planner/contract`, not a local copy.
- Interface or scheduling-order changes belong in the parent coordinator
  package and need parent service tests.

## Failure modes

- A duplicate `scope_id` fails planning before any work item is returned.
- A blank, path-like, or unsupported plan key fails through
  `contract.ValidateSafePlanKey`.
- Invalid or disabled collector configuration fails through
  `workflow.ValidatePackageRegistryCollectorConfiguration`.
- A malformed `instance.Configuration` during derivation degrades to no
  derived targets rather than failing the plan
  (`decodedPackageRegistryDerivation` swallows the decode error).

## Verification

Run focused child tests, then the recursive coordinator tree. Build and vet
the whole module because `cmd/workflow-coordinator` owns the concrete wiring,
and every import of this package needs its alias to compile.
