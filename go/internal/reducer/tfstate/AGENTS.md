# internal/reducer/tfstate Agent Rules

These rules are mandatory for changes under `go/internal/reducer/tfstate`.

## Read first

1. `README.md` and `doc.go`.
2. `go/internal/reducer/README.md` and `go/internal/reducer/AGENTS.md`.
3. `docs/internal/agent-guide.md` section "Bootstrap And Correlation Truth"
   before changing readiness checkpoints.

## Mandatory Invariants

- This package names the reducer-facing Terraform-state contract only.
  Runtime projection lives in `internal/projector`; collector parsing lives in
  `internal/collector/terraformstate`.
- The accepted Phase 1 checkpoints are `terraform_resource_uid` and
  `terraform_module_uid` at `canonical_nodes_committed`.
- Any domain consuming `resolved_relationships` derived from these nodes needs
  a post-Phase-3 reopen outside this package.
- `Validate` enforces non-blank fields; it does not check implementation
  presence.
- `DefaultRuntimeContract` and `RuntimeContractTemplate` MUST return defensive
  copies.

## Change Rules

- New component: update the runtime contract, README component list, and
  contract assertions together.
- New checkpoint: update the runtime contract, README checkpoint table, and
  tests. If the checkpoint is beyond Phase 1, document the post-Phase-3 reopen
  requirement here.

## Failure modes

- **Contract drift**: if `Validate` passes on an outdated contract, downstream
  wiring misses required checkpoints silently. Treat failing `Validate` in
  tests as a hard stop.

## Anti-Patterns

- Do not add live projection code to this package. Source-local Terraform-state
  node projection belongs in `internal/projector`; cross-source correlation
  belongs in a reducer handler registered with `internal/reducer.NewDefaultRegistry`.
- Do not export types that reference concrete graph backend types.

## Forbidden Without Architecture-Owner Approval

- The two accepted checkpoints (`terraform_resource_uid` and
  `terraform_module_uid` at `canonical_nodes_committed`). These define the
  Phase 1 readiness signal consumed by DSL evaluation.
- The component list, which is referenced in contract fixture assertions.
