# #5167 ledger disclosure: no runtime change

This note exists because the performance-evidence gate is content-based: it
flagged `go/internal/query/auth_scoped_routes_pending_row_filtering.go` and
`go/internal/query/openapi/paths/code/routes.go` as hot files for this branch, for
prose, not code. The ledger comment for `GET /api/v0/index-status` quotes the
unfiltered `MATCH (r:Repository) RETURN count(r)` that `getIndexStatus` runs,
to say why the route cannot bind a grant; the OpenAPI fragment gained a `403`
response and new description sentences. Neither file changes code.

## What the branch changes

- The seven MCP tool descriptions come from main (#6570); this branch adds only
  the `dead_code` note on `analyze_code_relationships`, and its contract-matrix
  rows mirror #6570's wording.
- The pending row-filtering ledger gains reason comments for three routes,
  corrects the impact family's note (the walks are bounded), points
  tag-history at #6564, and states the issue's terminal state. Two test files
  change comments only.
- Eight OpenAPI operations over seven routes declare the `403` they already
  return through the shared `Forbidden` component, with no
  `x-scoped-token-support` or `x-shared-key-only` marker; `http-api.md` and
  `http-api/code.md` describe the same.

## Why it is safe

No-Regression Evidence: no Cypher, SQL, handler, middleware or route-table
text changes. No route moved between the allowlist, the shared-key-only ledger
and the pending ledger: `git diff` from the merge base `e55bcef7c` over the
three `auth_scoped_routes_*` ledger files (`completeness`,
`pending_row_filtering`, `impact`) leaves the first untouched and changes only
comments in the other two, gofmt realigning two unchanged map keys aside.
`go test ./internal/query ./internal/mcp/... ./internal/ask/engine -count=1`
passes (38 packages); `TestPolicyGatedRoutesDeclareForbiddenResponse`,
`TestScopedTokenAllowlistCompleteness`,
`TestPendingRowFilteringRoutesDisjointFromScopedAndSharedKey` and
`TestEveryMCPReachableRouteIsScopedOrAnnotated` pin the current classification
(every route covered, the three sets disjoint), which a moved route would still
pass. `scripts/verify-openapi.sh` reports the same 255 routes and 255 path
entries as main. No read runs differently; the only measurable deltas are a
larger `tools/list` payload and a larger OpenAPI document, and neither carries
a query.

No-Observability-Change: no metric, span, log or status field is added or
removed; the 403 was already emitted by `scopedRouteDeniedResponse` and
audited by `recordScopedRouteAuthorizationDeniedWithReason`.
