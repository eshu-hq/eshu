# canonicalwriter

Adapts `tfstatebackend.Resolver.ResolveConfigCommitForBackend` to the
`(repoID string, outcome projector.TerraformStateOwnershipOutcome)` contract
`sourcecypher.TerraformStateOwnershipResolver` needs (#5623 P1 review, second
finding).

## Why this package exists

`cmd/bootstrap-index`, `cmd/ingester`, and `cmd/projector` each wire a
`*tfstatebackend.Resolver` into `cypher.CanonicalNodeWriter` via
`WithTerraformStateOwnershipResolver`. Before this package, each of the three
carried its own byte-identical adapter that mapped a
`ResolveConfigCommitForBackend` result into `(string, bool)`. A prior version
of that mapping collapsed three distinct outcomes into the same "not
resolved" signal:

- an ordinary transient failure (a Postgres timeout or pool exhaustion) —
  should PRESERVE any existing `MATCHES_STATE` edge, since this cycle's
  silence carries no information about whether that edge is still correct;
- `tfstatebackend.ErrNoConfigRepoOwnsBackend` — an AUTHORITATIVE "no owner"
  answer that should RETRACT any existing edge;
- `tfstatebackend.ErrAmbiguousBackendOwner` — an AUTHORITATIVE "not uniquely
  owned" answer that should also RETRACT any existing edge.

Losing that distinction reintroduced the exact tenant-visibility leak #5623
closed, through a narrower door: a backend that later became unowned or
ambiguous kept its prior owner's `MATCHES_STATE` edge indefinitely, and the
scoped-token infra predicate kept authorizing that repo.

`ResolveOwningRepoIDOutcome` is now the single place this classification
lives. Every `cmd/*` adapter is a thin wrapper that delegates to it, so the
three call sites cannot drift out of consistency with each other again.

## Why a separate package, not inside `tfstatebackend`

`projector` (owner of `TerraformStateOwnershipOutcome`) already transitively
imports `tfstatebackend` (`projector -> reducer ->
correlation/drift/tfconfigstate -> relationships/tfstatebackend`), so
`tfstatebackend` cannot import `projector` back without creating a cycle —
confirmed by an actual `go build` failure while developing this fix, not a
theoretical concern. This package sits above both dependencies as a thin
leaf, so it can import each directly without either needing to know about the
other, and so `internal/storage/cypher` (home of the
`TerraformStateOwnershipResolver` interface) keeps its documented narrow-port
boundary: it depends only on `projector` types, never on `tfstatebackend`
directly or transitively through this package.

## Contract

- `ResolveOwningRepoIDOutcome(ctx, resolver, backendKind, locatorHash) (string, projector.TerraformStateOwnershipOutcome)`
  runs the resolve call and classifies the result. `repoID` is non-empty only
  when the outcome is `TerraformStateOwnershipResolved`.
- Logs a warning for a genuine transient failure only — never for the two
  documented, expected "no owner" / "ambiguous owner" outcomes.

## Verification

`go test ./internal/relationships/tfstatebackend/canonicalwriter -count=1`
covers all four outcomes (resolved, no owner, ambiguous owner, transient
failure) against a fake `TerraformBackendQuery`.
