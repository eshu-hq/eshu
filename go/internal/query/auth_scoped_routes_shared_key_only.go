// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// sharedKeyOnlyRoutes is the #5167 Group C ledger: the structured,
// hand-maintained set of every HTTP API route that is MCP-reachable but can
// never be generically tenant-filtered, so it is explicitly excluded from
// scopedHTTPRouteSupportsTenantFilter (auth_scoped_routes.go) rather than
// silently missing from it. Each key is exactly the "METHOD /path" surface
// name capabilitycatalog.LoadSurfaceInventory() reports (see
// scopedTokenAdvertisedRoutes's doc comment for the same convention).
//
// A route belongs here, not on the scoped-token allowlist, when its operation
// requires authority that cannot be reduced to the caller's repository/scope
// grants. The Cypher and visualize routes execute caller-supplied graph
// programs with no bounded selector to intersect against a grant. The
// suppression route is bounded, but it creates operator-wide policy evidence
// that can change default finding visibility across scopes, so only an
// all-scopes caller may invoke it. A scoped or browser-session caller gets a
// 403 from AuthMiddleware; the explicit ledger keeps each exclusion
// staleness-checked instead of leaving an unannotated gap.
//
// The companion OpenAPI marker is "x-shared-key-only": true, declared next to
// each route's operation entry in its openapi_paths_*.go source (mirroring
// "x-scoped-token-support" and "x-browser-session-only" -- see
// auth_scoped_routes_completeness.go and
// auth_scoped_routes_completeness_test.go for the three-marker mutual
// exclusivity and ledger-agreement checks
// TestScopedTokenAllowlistCompleteness enforces). IsSharedKeyOnlyRoute is the
// exported, request-shaped accessor
// TestEveryMCPReachableRouteIsScopedOrAnnotated (go/internal/mcp) checks
// directly, since raw dispatched requests never carry OpenAPI path templates.
var sharedKeyOnlyRoutes = map[string]struct{}{
	"POST /api/v0/code/cypher":                      {},
	"POST /api/v0/code/visualize":                   {},
	"POST /api/v0/supply-chain/impact/suppressions": {},
}

// IsSharedKeyOnlyRoute reports whether r targets a route explicitly annotated
// as shared-key/all-scope only (#5167 Group C): either an unbounded program
// execution route or a bounded operator-wide mutation whose authority cannot
// be represented by repository/scope grants. It is exported so the
// MCP-reachability exhaustiveness gate in go/internal/mcp -- which resolves
// MCP tool calls to concrete dispatched requests, not OpenAPI templates -- can
// classify a route as intentionally excluded rather than an unannotated gap.
//
// The lookup is method-agnostic (the map key encodes the method), mirroring
// IsPendingRowFilteringRoute. An earlier POST-only early return was redundant
// while every entry is a POST, but it would silently misclassify a future
// non-POST shared-key route as unclassified and trip the exhaustiveness gate,
// so it is intentionally absent.
func IsSharedKeyOnlyRoute(r *http.Request) bool {
	_, ok := sharedKeyOnlyRoutes[r.Method+" "+r.URL.Path]
	return ok
}
