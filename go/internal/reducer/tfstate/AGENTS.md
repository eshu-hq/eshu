# AGENTS — internal/reducer/tfstate

This file guides LLM assistants working in `go/internal/reducer/tfstate`.
Read it before touching any file in this directory.

## Read first

1. `go/internal/reducer/README.md` — full reducer context and the
   post-Phase-3 reopen requirement.
2. `go/internal/reducer/AGENTS.md` — invariants governing all reducer
   sub-packages.
3. `CLAUDE.md` "Facts-First Bootstrap Ordering" — Phase 1 canonical-nodes
   publications from this scaffold feed downstream domains that may require
   Phase 3 reopen.

## Invariants (cite file:line)

- **Contract only** — `doc.go:1–9` is explicit: this package names the
  reducer-facing Terraform-state contract. Runtime projection lives in
  `internal/projector`; collector parsing lives in
  `internal/collector/terraformstate`.
- **Two Phase 1 checkpoints** — `contract.go:27–40`; both
  `terraform_resource_uid` and `terraform_module_uid` target
  `canonical_nodes_committed`. These are Phase 1 publications; any domain
  consuming `resolved_relationships` derived from these nodes needs a
  post-Phase-3 reopen (`bootstrap-index/main.go:273`).
- **`Validate` enforces non-blank fields** — `contract.go:55–77`; it does
  not check implementation presence.
- **Defensive copies from factory functions** — `contract.go:43–53`; both
  `DefaultRuntimeContract` and `RuntimeContractTemplate` use `slices.Clone`.

## Common changes

### Add a new component to the contract

1. Append to `defaultRuntimeContract.Components` in `contract.go`.
2. Update the README component list in the same PR.
3. Add a `contract_test.go` assertion.

### Add a new checkpoint

1. Add a `PublishedCheckpoint` entry to `defaultRuntimeContract.Checkpoints`
   in `contract.go`.
2. If the new checkpoint is beyond Phase 1, document the post-Phase-3 reopen
   requirement here.

## Failure modes

- **Contract diverging from ADR**: if `Validate` passes on an outdated
  contract, downstream wiring misses required checkpoints silently. Treat
  failing `Validate` in tests as a hard stop.

## Anti-patterns

- Do not add live projection code to this package. Source-local Terraform-state
  node projection belongs in `internal/projector`; cross-source correlation
  belongs in a reducer handler registered with `internal/reducer.NewDefaultRegistry`.
- Do not export types that reference concrete graph backend types.

## What NOT to change without an ADR

- The two accepted checkpoints (`terraform_resource_uid` and
  `terraform_module_uid` at `canonical_nodes_committed`). These define the
  Phase 1 readiness signal consumed by DSL evaluation.
- The component list, which is referenced in ADR fixture assertions.
