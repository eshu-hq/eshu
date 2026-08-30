# MCP container-image identity route selection

## Purpose

This package owns family membership and pure internal-request selection for the
four MCP container-image identity tools: the cursor-paged listing of which
digest a deployed image reference resolves to, the ordered history of how a
repository:tag's digest changed, and the counted and grouped summaries over the
same identity facts.

## Ownership boundary

This package owns container-image family membership and the mapping from
decoded arguments to a dependency-neutral internal request. `internal/mcp`
keeps tool registration and its client-visible order, global route fanout, the
private adapter, HTTP dispatch, authorization, timeouts, response budgets,
envelopes, summaries, and telemetry. `internal/query` owns the bounded reads
these paths reach: `SupplyChainHandler` for the identity listing and the two
aggregates, `TagHistoryHandler` for the tag history, including every limit
bound, offset ceiling, scope-anchor requirement, and `group_by` validation.

## Exported surface

- `Route` selects the internal request for a container-image tool without
  executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument and
  internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, and the HTTP handlers retain the shared
API request duration and error metrics plus, for tag history, the
handler-scoped duration and error datapoints in `internal/query`.

## Gotchas / invariants

- The import path ends in `containerimage`, while the declared package is
  `containerimagetools`. The root uses an explicit import alias.
- Tag history does not share the family's path prefix. Three tools resolve
  under `/api/v0/supply-chain/container-images/identities`; tag history
  resolves to `/api/v0/images/tag-history`, which is where
  `TagHistoryHandler.Mount` registers it. Folding it onto the sibling prefix
  reads like tidying and selects a path the query mux does not serve.
- The four key sets are deliberately unequal. The listing carries
  `after_identity_id`, `digest`, `image_ref`, `limit`, `outcome`,
  `repository_id`, and `source_repository_id`. Tag history carries `limit`,
  `offset`, `repository_id`, and `tag`. The count carries the five filters
  `digest`, `image_ref`, `source_repository_id`, `repository_id`, and
  `outcome`. The inventory carries those five plus `group_by`, `limit`, and
  `offset`.
- The count route has no paging key. Adding one for symmetry with its three
  siblings would not bound anything, because the handler never reads it; the key
  would be inert and would advertise a bound the endpoint does not honor.
- Every key a route owns is sent even when the caller omitted it, so the
  handler sees an explicitly empty filter rather than no filter key at all.
- A dropped key does not fail uniformly. On the identity listing, `limit` is
  required and a scope anchor is required — one of `digest`, `image_ref`,
  `source_repository_id`, `repository_id`, `outcome` — so losing either returns
  400, except that a scoped token with no grants is answered with an empty page
  before the anchor is checked; losing `after_identity_id` breaks keyset paging
  silently and re-serves page one. On tag history, `repository_id` and `tag` are both required and
  compose the anchoring `image_ref`, so losing either returns 400, while
  `limit` and `offset` are optional at the handler. On the two aggregates
  nothing is required, so a lost filter returns 200 over a wider scope and
  drops that key from the `scope` block the response echoes back.
- `limit` defaults to 50 on the listing and on tag history, and to 100 on the
  inventory. `offset` defaults to 0 on tag history and the inventory. These are
  the dispatcher's historical defaults; the handlers still enforce their own
  bounds (1-200 for the listing and tag history, 1-500 and a 10000 offset
  ceiling for the inventory).
- The `group_by` fallback to `outcome` is not what makes an omitted dimension
  work: `containerImageIdentityInventory` independently defaults an empty
  `group_by` to `outcome`. The fallback keeps the selected wire value stable,
  so changing it to another dimension would change the grouping the caller
  receives. An unsupported value is forwarded verbatim so the handler answers
  with its own 400 rather than the route correcting a typo into a valid
  grouping.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`, and
  `float64` are accepted, a `float64` truncates toward zero, and every other
  type falls back to the default.
- `Route` returns a fresh query map per call, so a caller may mutate one result
  without changing a later one or the arguments it was given.

No-Observability-Change: this extraction moves only pure container-image route
selection. The root adapter still feeds the same global fanout, dispatch,
authorization, budgets, envelopes, summaries, and transport telemetry, and the
same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
