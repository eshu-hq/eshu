// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// request builds the minimal request the route ledgers classify: method plus
// path, the only two fields every Group C predicate reads.
func request(method, path string) *http.Request {
	return httptest.NewRequest(method, path, nil)
}

// TestDrainedGroupCLedgers pins the row-2 leaf-2b (#9669) move: the Group C
// auth ledgers live here now, with the same entries and decisions the root
// package (and the MCP exhaustiveness gate) relied on.
func TestDrainedGroupCLedgers(t *testing.T) {
	t.Run("shared key only ledger", func(t *testing.T) {
		for _, surface := range []string{
			"POST /api/v0/code/cypher",
			"POST /api/v0/code/visualize",
			"POST /api/v0/supply-chain/impact/suppressions",
		} {
			if _, ok := SharedKeyOnlyRoutes[surface]; !ok {
				t.Errorf("SharedKeyOnlyRoutes is missing %q", surface)
			}
			if !IsSharedKeyOnlyRoute(request("POST", pathOf(surface))) {
				t.Errorf("IsSharedKeyOnlyRoute(%q) = false, want true", surface)
			}
		}
		if IsSharedKeyOnlyRoute(request("GET", "/api/v0/repositories")) {
			t.Error("IsSharedKeyOnlyRoute(GET /api/v0/repositories) = true, want false")
		}
	})
	t.Run("pending row filtering ledger stays closed", func(t *testing.T) {
		if len(PendingRowFilteringRoutes) != 0 {
			t.Errorf("len(PendingRowFilteringRoutes) = %d, want 0 (every Group B route promoted; the ledger stays as the closed gate, not an empty accident)", len(PendingRowFilteringRoutes))
		}
		if IsPendingRowFilteringRoute(request("GET", "/api/v0/repositories")) {
			t.Error("IsPendingRowFilteringRoute(GET /api/v0/repositories) = true, want false")
		}
	})
	t.Run("allowlist decision", func(t *testing.T) {
		allowlisted := [][2]string{
			{"GET", "/api/v0/repositories"},
			{"POST", "/api/v0/ask"},
			{"GET", "/sse"},
			{"POST", "/mcp/message"},
		}
		for _, probe := range allowlisted {
			if !ScopedHTTPRouteSupportsTenantFilter(request(probe[0], probe[1])) {
				t.Errorf("ScopedHTTPRouteSupportsTenantFilter(%s %s) = false, want true", probe[0], probe[1])
			}
		}
		if ScopedHTTPRouteSupportsTenantFilter(request("POST", "/api/v0/code/cypher")) {
			t.Error("ScopedHTTPRouteSupportsTenantFilter(POST /api/v0/code/cypher) = true, want false")
		}
	})
	t.Run("surfaces agree with ledgers", func(t *testing.T) {
		mcpSurfaces := SharedKeyOnlyRouteSurfaces()
		sort.Strings(mcpSurfaces)
		want := []string{"POST /api/v0/code/cypher", "POST /api/v0/code/visualize"}
		sort.Strings(want)
		if len(mcpSurfaces) != len(want) {
			t.Fatalf("SharedKeyOnlyRouteSurfaces() = %v, want %v", mcpSurfaces, want)
		}
		for i := range want {
			if mcpSurfaces[i] != want[i] {
				t.Fatalf("SharedKeyOnlyRouteSurfaces() = %v, want %v", mcpSurfaces, want)
			}
		}
		if len(PendingRowFilteringRouteSurfaces()) != 0 {
			t.Errorf("PendingRowFilteringRouteSurfaces() = %v, want empty", PendingRowFilteringRouteSurfaces())
		}
	})
	t.Run("route policy stays fail closed", func(t *testing.T) {
		var closed BrowserSessionRoutePolicy
		if closed.AllowTenantBoundAllScopes {
			t.Fatal("zero BrowserSessionRoutePolicy admits, want fail-closed")
		}
		tenantBound := AuthContext{Mode: AuthModeBrowserSession, TenantID: "tenant_a", WorkspaceID: "workspace_a", AllScopes: true}
		if got := BrowserSessionRouteDenialReason(request("POST", "/api/v0/code/cypher"), tenantBound, BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true}); got == "" {
			t.Error("shared-key-only route admitted for a tenant-bound all-scope session, want a denial reason")
		}
		grantBound := request("GET", "/api/v0/repositories")
		if got := BrowserSessionRouteDenialReason(grantBound, tenantBound, BrowserSessionRoutePolicy{}); got == "" {
			t.Error("grant-bound route admitted under the zero policy, want a denial reason")
		}
		restricted := AuthContext{Mode: AuthModeBrowserSession, TenantID: "tenant_a", WorkspaceID: "workspace_a"}
		if got := BrowserSessionRouteDenialReason(grantBound, restricted, BrowserSessionRoutePolicy{}); got != "" {
			t.Errorf("restricted session denied grant-bound route with %q, want admission", got)
		}
	})
	t.Run("denial signal install mark read", func(t *testing.T) {
		ctx, denied := WithScopedRouteDenialSignal(context.Background())
		if denied() {
			t.Fatal("fresh signal reads denied, want false")
		}
		MarkScopedRouteDenied(ctx)
		if !denied() {
			t.Fatal("marked signal reads admitted, want true")
		}
		MarkScopedRouteDenied(context.Background())
	})
}

// pathOf splits a "METHOD /path" surface name back to its path half.
func pathOf(surface string) string {
	for i := len(surface) - 1; i >= 0; i-- {
		if surface[i] == ' ' {
			return surface[i+1:]
		}
	}
	return surface
}
