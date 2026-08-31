# MCP security-alert reconciliation route selection

## Purpose

This package owns family membership and pure internal-request selection for
the three MCP security-alert reconciliation tools: the cursor-paged listing
of reducer-owned provider security-alert reconciliations, plus its
whole-scope count and grouped inventory.

## Ownership boundary

This package owns security-alert reconciliation family membership and the
mapping from decoded arguments to a dependency-neutral internal request.
`internal/mcp` keeps tool registration and its client-visible order, global
route fanout, the private adapter, HTTP dispatch, authorization, timeouts,
response budgets, envelopes, summaries, and telemetry. `internal/query` owns
the bounded reads these paths reach: `SupplyChainHandler
.listSecurityAlertReconciliations` for the listing and
`countSecurityAlertReconciliations` / `securityAlertReconciliationInventory`
for the two aggregates, including every limit bound, offset ceiling, scope
requirement, and `group_by` validation.

## Exported surface

- `Route` selects the internal request for a security-alert reconciliation
  tool without executing it, and reports `handled=false` for every other
  tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument
  and internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP
package keeps transport and dispatch signals, and the HTTP handlers retain
the shared API request duration and error metrics in `internal/query`.

## Gotchas / invariants

- The import path ends in `securityalert`, while the declared package is
  `securityalerttools`. The root uses an explicit import alias.
- The three key sets are deliberately unequal. The listing carries the nine
  keys `after_reconciliation_id`, `cve_id`, `ghsa_id`, `limit`, `package_id`,
  `provider`, `provider_state`, `reconciliation_status`, and
  `repository_id`. The count and the inventory share those same filters
  minus `after_reconciliation_id` and `limit`; the inventory adds
  `group_by`, `limit`, and `offset`.
- The count route has no paging key. Adding one for symmetry with the
  listing and the inventory would not bound anything, because the handler
  never reads it; the key would be inert and would advertise a bound the
  endpoint does not honor.
- The listing requires `limit` and one of `repository_id`, `provider`,
  `package_id`, `cve_id`, or `ghsa_id` as a scope anchor -- `provider_state`
  and `reconciliation_status` do not count as anchors on their own -- except
  that an empty scoped-token grant is answered with an empty page before the
  anchor is checked. The count and the inventory require nothing.
- Every key a route owns is sent even when the caller omitted it, so the
  handler sees an explicitly empty filter rather than no filter key at all.
- `limit` defaults to 50 on the listing and to 100 on the inventory. `offset`
  defaults to 0 on the inventory. These are the dispatcher's historical
  defaults; the handlers still enforce their own bounds.
- The `group_by` fallback to `reconciliation_status` is not what makes an
  omitted dimension work: the handler independently defaults an empty
  `group_by` to `reconciliation_status`. The fallback keeps the selected
  wire value stable, so changing it to another dimension would change the
  grouping every ungrouped caller receives.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`,
  and `float64` are accepted, a `float64` truncates toward zero, and every
  other type falls back to the default.
- `Route` returns a fresh query map per call, so a caller may mutate one
  result without changing a later one or the arguments it was given.

No-Observability-Change: this extraction moves only pure security-alert
reconciliation route selection. The root adapter still feeds the same global
fanout, dispatch, authorization, budgets, envelopes, summaries, and transport
telemetry, and the same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [MCP supply-chain-impact route selection](../supplychainimpact/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
