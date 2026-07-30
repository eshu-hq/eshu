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
theoretical concern (independently reproduced by adding a throwaway import,
per the #5623 P1 follow-up review). This package sits above both dependencies
as a thin leaf, so it can import each directly without either needing to know
about the other.

That keeps `internal/storage/cypher`'s `TerraformStateOwnershipResolver`
interface itself scoped to `projector` types only: it does not import
`tfstatebackend`, and this package adds ZERO new transitive edge from `cypher`
to `tfstatebackend` beyond what already existed. Precision matters here
because `internal/storage/cypher` already depends on `tfstatebackend`
transitively through a DIFFERENT, PRE-EXISTING, unrelated path this package
did not create: `edge_writer.go` (in `internal/storage/cypher`) imports
`internal/reducer`, and `internal/reducer` imports `tfstatebackend` directly
(for example `terraform_config_state_drift.go`, wiring the drift-correlation
resolver `cmd/reducer/wiring_handlers.go` already uses) — confirmed by `go
list -deps ./internal/storage/cypher` showing
`github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend` in the
output. That edge predates this branch entirely and has nothing to do with
`TerraformStateOwnershipResolver`. A reader who runs `go list -deps` and finds
`tfstatebackend` in `cypher`'s transitive closure should not read that as a
regression introduced by this package — it was already there, for an
unrelated reason, before #5623 existed.

## Contract

- `ResolveOwningRepoIDOutcome(ctx, resolver, backendKind, locatorHash) (string, projector.TerraformStateOwnershipOutcome)`
  runs the resolve call and classifies the result. `repoID` is non-empty only
  when the outcome is `TerraformStateOwnershipResolved`.
- Logs a warning for a genuine transient failure only — never for the two
  documented, expected "no owner" / "ambiguous owner" outcomes.

**`AmbiguousOwner` can itself be a byproduct of eventually-consistent
ingestion, not only a genuinely contested backend.** The underlying query
(`PostgresTerraformBackendQuery.ListTerraformBackendsByLocator`,
`internal/storage/postgres/tfstate_backend_canonical.go`) joins each
candidate `terraform_backends` fact through
`scope.active_generation_id = fact.generation_id` — it only sees each repo's
CURRENTLY-ACTIVE generation, and repos re-ingest independently and
asynchronously. During a real backend-ownership migration (state moved from
repo A to repo B), if A's next ingestion (dropping its now-stale declaration)
lags B's (picking up the new one), the resolver observes both as active
claimants and returns `TerraformStateOwnershipAmbiguousOwner` — not because
ownership is contested, but because ingestion has not converged yet. The
caller (`terraformStateMatchesConfigEdgeRetractStatements`,
`internal/storage/cypher`) treats this the same as a genuinely contested
backend and retracts any existing edge, so scoped-token visibility can flap
during the migration window. This is judged acceptable (fails safe,
self-heals, and the window is bounded by ordinary delta-ingestion cadence
rather than the multi-hour full-reconciliation interval the #5623 P0 leak was
bounded by) — see the full reasoning and evidence in
`internal/storage/cypher/AGENTS-evidence-history.md`'s `#5623 P1 follow-up`
entry.

## Verification

`go test ./internal/relationships/tfstatebackend/canonicalwriter -count=1`
covers all four outcomes (resolved, no owner, ambiguous owner, transient
failure) against a fake `TerraformBackendQuery`.
