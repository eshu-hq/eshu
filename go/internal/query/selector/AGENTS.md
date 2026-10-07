# Agent instructions: selector

Read `doc.go` and `README.md` first. This resolves an untrusted client string
against the graph under authorization bounds, so treat changes as security work.

## Invariants

- `ResolveExactForAccess` MUST return `NotFoundError` when `access.Empty()`,
  before touching the graph. Removing that check lets a caller with no grants
  resolve any repository.
- The access predicate MUST stay in both queries. `access.GraphPredicate("r")`
  and `access.GraphParams(...)` are what bind the read to the caller's grants;
  a query that drops either still compiles and returns other tenants' rows.
- Both reads MUST stay parameterised (`$repo_selector`). The selector is
  client-supplied; interpolating it into the Cypher is an injection.
- The ordered-then-fallback pair is deliberate. Do not collapse it.
- Every backing-read failure in `ResolveExactForAccess` (the catalog read and
  both graph reads) MUST return a `LookupError` that wraps the backend error
  with `%w`. The wrap is what lets callers tell a server fault (500) from a
  selector answer (404/400), and `Unwrap` is what lets
  `querycontract.WriteGraphReadError` still map fence and graph-availability
  verdicts to 503/504 first. Do not add the selector to `LookupError` or its
  text, and do not return a `LookupError` for a not-found or ambiguous answer
  (#7626).
- `ResolveForRequestWithAccess` MUST keep the order: `WriteGraphReadError`,
  then `WriteLookupFailure` (500, fixed body, span error), then `IsNotFound`
  (404), then 400. Callers that map selector errors themselves use the same
  order and the same helper; do not inline a second copy of the 500 answer.
- `HydrateResolvedEntityRepoIdentity` MUST call
  `entity.ClearResolvedEntityRepoProjectionPlaceholders` on every
  entity before any other hydration path runs (#6408). Skipping it lets a
  leaked backend projection expression stand in as a real repository id.
- The workload-backfill query's two access splices
  (`access.GraphPredicate("repo")` on the direct-DEFINES branch,
  `access.GraphWhereClause("repoViaInstance")` on the via-instance branch)
  MUST both stay. Dropping either lets a scoped caller's workload backfill
  read another tenant's repository identity.

## When you change the query text

`ResolveExactForAccess` is pinned in
`go/internal/queryplan/testdata/query-source-coverage.yaml` with a typed
`non_hot` disposition (`class: keyed_support`, `key_bound: single_key`,
`max_results: 51`) and a `source_sha256` over the function source from the
`func` keyword to its closing brace. It is not in `grandfathered_non_hot.go`.

Any edit to that function body, not only to the query text, fails the coverage
gate; a doc-comment edit does not. There is no regenerator: re-pin by hand-editing
`source_sha256` to the production digest the failing gate prints. Before
re-pinning, prove the Cypher itself did not change (extract the Cypher string
literals with `go/parser` before and after and compare their digest). If the
query DID change, the disposition needs a real audit, not a re-pin.

## Verification

From `go/`: `go test ./internal/query/... ./internal/queryplan -count=1`.
Confirm the selector suite ran a real case count rather than matching zero.
