# #5167 ledger disclosure: no runtime change

This note exists because the performance-evidence gate is content-based: it
flagged `go/internal/query/auth_scoped_routes_pending_row_filtering.go` and
`go/internal/query/openapi_paths_code.go` as hot files for this branch. Both
were flagged for prose, not code. The ledger comment for
`GET /api/v0/index-status` quotes the unfiltered `MATCH (r:Repository)
RETURN count(r)` that `getIndexStatus` runs, to say why the route cannot bind a
grant; the OpenAPI fragment gained a `403` response and a description
sentence. Neither file changes a statement, a handler, a route table or a
test assertion.

## What the branch changes

- Seven MCP tool descriptions and their contract-matrix rows say that scoped
  tokens are refused with a 403 and why.
- The pending row-filtering ledger gains reason comments for three routes,
  corrects the impact family's note (the walks are bounded), points
  tag-history at #6564, and states the terminal state the issue's scope text
  defines. Two test files change comments only.
- Eight OpenAPI operations over seven routes declare the `403` they already
  return, through the shared `Forbidden` component, without any
  `x-scoped-token-support` or `x-shared-key-only` marker.
- `http-api.md` and `http-api/code.md` describe the same.

## Why it is safe

No-Regression Evidence: no Cypher, SQL, handler, middleware or route-table
text changes; every statement the flagged files reference is quoted in a
comment or an OpenAPI description. Baseline and after are the same binaries
for every request path: `go test ./internal/query ./internal/mcp/...
./internal/ask/engine -count=1` passes (38 packages) at the branch head, and
`TestPolicyGatedRoutesDeclareForbiddenResponse`,
`TestScopedTokenAllowlistCompleteness`,
`TestPendingRowFilteringRoutesDisjointFromScopedAndSharedKey` and
`TestEveryMCPReachableRouteIsScopedOrAnnotated` pin that no route moved
between the allowlist, the shared-key-only ledger and the pending ledger.
`scripts/verify-openapi.sh` reports the same 255 routes and 255 path entries
as main. Input shape, row counts and backend version are unchanged because no
read runs differently; the only measurable delta is a larger `tools/list`
payload from longer descriptions, which carries no query.

No-Observability-Change: no metric, span, log or status field is added or
removed; the 403 these routes return was already emitted by
`scopedRouteDeniedResponse` and already audited by
`recordScopedRouteAuthorizationDeniedWithReason`.
