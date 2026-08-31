# MCP supply-chain-impact route selection

## Purpose

This package owns family membership and pure internal-request selection for
the four MCP supply-chain-impact tools: the cursor-paged listing of
reducer-owned vulnerability findings, its whole-scope count and grouped
inventory, and the single bounded explanation of why one package, image, or
workload is or is not affected by a given advisory or CVE.

## Ownership boundary

This package owns supply-chain-impact family membership and the mapping from
decoded arguments to a dependency-neutral internal request. `internal/mcp`
keeps tool registration and its client-visible order, global route fanout,
the private adapter, HTTP dispatch, authorization, timeouts, response
budgets, envelopes, summaries, and telemetry. `internal/query` owns the
bounded reads these paths reach: `SupplyChainHandler.listImpactFindings`,
`countImpactFindings`, and `impactInventory` for the listing and the two
aggregates, and `explainImpact` for the explanation, including every limit
bound, offset ceiling, scope requirement, and `group_by` validation.

## Exported surface

- `Route` selects the internal request for a supply-chain-impact tool without
  executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument
  and internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP
package keeps transport and dispatch signals, and the HTTP handlers retain
the shared API request duration and error metrics in `internal/query`.

## Gotchas / invariants

- The import path ends in `supplychainimpact`, while the declared package is
  `supplychainimpacttools`. The root uses an explicit import alias.
- The four key sets are deliberately unequal. The listing carries the twenty
  keys `advisory_id`, `after_finding_id`, `cve_id`, `ecosystem`,
  `environment`, `ghsa_id`, `image_ref`, `impact_status`, `limit`,
  `min_priority_score`, `osv_id`, `package_id`, `priority_bucket`, `profile`,
  `repository_id`, `service_id`, `severity`, `sort`, `subject_digest`,
  `suppression_state`, and `workload_id`, plus `include_suppressed` when the
  caller sets it. The count and the inventory share those same filters minus
  `after_finding_id` and `sort`; the inventory adds `group_by`, `limit`, and
  `offset`. The explanation carries a distinct nine keys:
  `advisory_id`, `cve_id`, `finding_id`, `image_ref`, `package_id`,
  `repository_id`, `service_id`, `subject_digest`, and `workload_id`.
- The count route has no paging key. Adding one for symmetry with the listing
  and the inventory would not bound anything, because the handler never reads
  it; the key would be inert and would advertise a bound the endpoint does
  not honor.
- `include_suppressed` is a three-state key: absent when the caller never set
  it, so the handler's documented `false` default applies; `"true"` or
  `"false"` when the caller set an explicit bool; and absent again for any
  other type. `routecontract.Arguments.BoolOr` cannot express this because it
  collapses "absent" into the caller's fallback, so this package carries its
  own `boolStr` helper rather than reusing `BoolOr`.
- Every key a route owns is sent even when the caller omitted it, so the
  handler sees an explicitly empty filter rather than no filter key at all.
- A dropped key does not fail uniformly. On the listing, `limit` is required
  and a scope anchor is required -- one of `cve_id`, `advisory_id`,
  `package_id`, `repository_id`, `subject_digest`, `image_ref`,
  `impact_status`, `ecosystem`, `workload_id`, `service_id`, `environment`,
  `severity`, `priority_bucket`, or a positive `min_priority_score` -- so
  losing either returns 400, except that a scoped token with no grants is
  answered with an empty page before the anchor is checked; losing
  `after_finding_id` breaks keyset paging silently and re-serves page one. On
  the explanation, `finding_id` alone or an advisory/CVE anchor plus one
  bounded scope leg is required, so losing the whole scope returns 400. On
  the count and the inventory nothing is required, so a lost filter returns
  200 over a wider scope and drops that key from the `scope` block the
  response echoes back.
- `limit` defaults to 50 on the listing and to 100 on the inventory. `offset`
  defaults to 0 on the inventory. These are the dispatcher's historical
  defaults; the handlers still enforce their own bounds (1-200 for the
  listing, 1-500 for the inventory with a 10000 offset ceiling).
- The `group_by` fallback to `impact_status` is not what makes an omitted
  dimension work: `impactInventory` independently defaults an empty
  `group_by` to `impact_status` and rejects anything outside
  `impact_status`, `priority_bucket`, `severity`, `repository_id`, and
  `ecosystem` with a 400. The fallback keeps the selected wire value stable,
  so changing it to another dimension would change the grouping the caller
  receives. An unsupported value is forwarded verbatim so the handler answers
  with its own 400 rather than the route silently correcting a typo into a
  valid grouping.
- `ghsa_id` and `osv_id` are forwarded as their own wire keys on the listing,
  the count, and the inventory. The handler folds an empty `advisory_id` back
  to whichever of `ghsa_id` or `osv_id` the caller sent; this package
  forwards all three keys unchanged and leaves that fallback to the handler.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`,
  and `float64` are accepted, a `float64` truncates toward zero, and every
  other type falls back to the default.
- `Route` returns a fresh query map per call, so a caller may mutate one
  result without changing a later one or the arguments it was given.

No-Observability-Change: this extraction moves only pure supply-chain-impact
route selection. The root adapter still feeds the same global fanout,
dispatch, authorization, budgets, envelopes, summaries, and transport
telemetry, and the same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [MCP container-image identity route selection](../containerimage/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
