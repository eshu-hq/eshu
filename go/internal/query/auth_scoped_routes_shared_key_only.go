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
// A route belongs here, not on the scoped-token allowlist, when its handler
// executes caller-supplied Cypher with no bounded shape to intersect against
// a grant: POST /api/v0/code/cypher (execute_cypher_query, handleCypherQuery)
// and POST /api/v0/code/visualize (visualize_graph_query, handleVisualizeQuery)
// both run h.Neo4j.Run/RunSingle against the caller's literal query text with
// no AllowedRepositoryIDs/AllowedScopeIDs binding anywhere in the call path --
// unlike every Group A/B route, there is no selector to filter, because the
// query itself is the untrusted input. These routes remain reachable only for
// shared-key and all-scope callers; a scoped or browser-session caller gets a
// 403 from AuthMiddleware, same as before this ledger existed, but now that
// exclusion is an explicit, staleness-checked declaration instead of an
// unannotated gap indistinguishable from an oversight.
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
	"POST /api/v0/code/cypher":    {},
	"POST /api/v0/code/visualize": {},
}

// IsSharedKeyOnlyRoute reports whether r targets a route explicitly annotated
// as shared-key/all-scope only (#5167 Group C): a route that clears no
// tenant-scope filter and never will, because its handler executes
// caller-supplied Cypher with nothing to bind a grant against. It is exported
// so the MCP-reachability exhaustiveness gate in go/internal/mcp -- which
// resolves MCP tool calls to concrete dispatched requests, not OpenAPI
// templates -- can classify a route as intentionally excluded rather than an
// unannotated gap.
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
