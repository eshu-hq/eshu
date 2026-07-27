// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// ScopedHTTPRouteSupportsTenantFilter is the exported form of
// scopedHTTPRouteSupportsTenantFilter (auth_scoped_routes.go), the same
// allowlist AuthMiddlewareWithScopedTokens checks before admitting a scoped
// or browser-session request. It exists so the #5167 MCP-reachability
// exhaustiveness gate in go/internal/mcp -- which needs both this package's
// allowlist decision and the mcp package's tool-to-route dispatch table, and
// therefore cannot live entirely inside either package's own test binary --
// can classify a dispatched request without duplicating the allowlist logic.
// query never imports mcp (mcp already imports query), so this accessor is
// the only direction such a check can cross the package boundary.
func ScopedHTTPRouteSupportsTenantFilter(r *http.Request) bool {
	return scopedHTTPRouteSupportsTenantFilter(r)
}

// httpOnlySharedKeyOnlyRoutes records shared-key/all-scope routes that are
// deliberately absent from the read-only MCP tool surface. Keeping this
// classification separate means a new shared-key-only route still fails the
// MCP staleness gate until its transport ownership is explicit.
var httpOnlySharedKeyOnlyRoutes = map[string]struct{}{
	"POST /api/v0/supply-chain/impact/suppressions": {},
}

// SharedKeyOnlyRouteSurfaces returns the MCP-reachable "METHOD /path" entries
// in sharedKeyOnlyRoutes (auth_scoped_routes_shared_key_only.go), for the mcp
// package's exhaustiveness gate to staleness-check against the routes it
// actually dispatches to. HTTP-only entries remain in the authorization ledger
// but are omitted here through the explicit classification above.
func SharedKeyOnlyRouteSurfaces() []string {
	surfaces := make([]string, 0, len(sharedKeyOnlyRoutes))
	for surface := range sharedKeyOnlyRoutes {
		if _, httpOnly := httpOnlySharedKeyOnlyRoutes[surface]; httpOnly {
			continue
		}
		surfaces = append(surfaces, surface)
	}
	return surfaces
}

// PendingRowFilteringRouteSurfaces returns every "METHOD /path" surface name
// in the literal pendingRowFilteringRoutes ledger
// (auth_scoped_routes_pending_row_filtering.go), for the mcp package's
// exhaustiveness gate to staleness-check against the routes it actually
// dispatches to.
func PendingRowFilteringRouteSurfaces() []string {
	surfaces := make([]string, 0, len(pendingRowFilteringRoutes))
	for surface := range pendingRowFilteringRoutes {
		surfaces = append(surfaces, surface)
	}
	return surfaces
}
