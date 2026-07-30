# AGENTS — tfstatebackend/canonicalwriter

Guidance for LLM assistants editing this package.

## Read first

1. `doc.go` and `README.md` in this directory — why this package exists (a
   real import-cycle boundary, not a style choice) and its one exported
   function's contract.
2. `../resolver.go` — `Resolver.ResolveConfigCommitForBackend` and the two
   error sentinels this package classifies.
3. `../../../projector/tfstate_ownership_outcome.go` — the
   `TerraformStateOwnershipOutcome` enum this package produces.
4. `../../../storage/cypher/tfstate_state_match_edge.go` — the
   `TerraformStateOwnershipResolver` interface every `cmd/*` adapter
   implements by delegating to this package.

## Invariants

- `ResolveOwningRepoIDOutcome` MUST return a non-empty `repoID` only when the
  outcome is `TerraformStateOwnershipResolved`. Every other outcome MUST
  return an empty `repoID` — never a guess.
- `TerraformStateOwnershipNoOwner` and `TerraformStateOwnershipAmbiguousOwner`
  are AUTHORITATIVE answers, not failures: do not fold them back into
  `TerraformStateOwnershipTransientFailure`. That collapse is the exact #5623
  P1 review defect this package fixes — reintroducing it silently reopens a
  tenant-visibility leak in `go/internal/storage/cypher`'s MATCHES_STATE
  retract logic.
- Do NOT import this package from `internal/projector` or
  `internal/relationships/tfstatebackend`. It exists specifically because
  those two cannot import each other's dependency of this package's outcome
  type without cycling; importing it back into either recreates the cycle
  this package was built to avoid.
- Every `cmd/{bootstrap-index,ingester,projector}` adapter MUST delegate to
  `ResolveOwningRepoIDOutcome` rather than re-implementing the classification
  inline. If a fourth `cmd/*` binary ever needs this resolver, wire it the
  same way.

## Common changes

- Add a new authoritative outcome: add the constant to
  `TerraformStateOwnershipOutcome` (projector package), classify it here,
  and update every `cmd/*` adapter's own doc comment (they intentionally
  cross-reference this package and each other).
- Change logging behavior: keep the "log only genuine transient failures"
  rule — `NoOwner` and `AmbiguousOwner` are common, expected, and already
  visible via `config_repo_id` staying null on the graph node.

## What NOT to change without architecture-owner approval

- The three-outcome-plus-resolved shape. Collapsing outcomes back together is
  exactly the defect this package fixes.
