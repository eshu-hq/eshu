// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// acceptAuthPolicy takes the auth-owned policy spelling. Passing the root
// spelling compiles only while the root name is an alias for the auth type,
// so this and acceptRootPolicy together pin the alias in both directions.
func acceptAuthPolicy(auth.BrowserSessionRoutePolicy) {}

// acceptRootPolicy takes the root policy spelling. See acceptAuthPolicy.
func acceptRootPolicy(BrowserSessionRoutePolicy) {}

// TestRoutePolicyDrainAliasesAreIdentical proves the D13 (#9669) row-2
// leaf-2b drain moved the Group C auth ledgers without changing them: every
// root name spells the exact auth-owned value, so middleware admission, the
// MCP classification gate, and the transport-auth denial labels behave
// byte-for-byte as before. It fails on drift in either direction: a root
// forwarder that stops delegating, or an auth ledger that stops matching
// the root spelling the tests below pin.
func TestRoutePolicyDrainAliasesAreIdentical(t *testing.T) {
	t.Run("ledger maps share storage", func(t *testing.T) {
		if len(sharedKeyOnlyRoutes) != len(auth.SharedKeyOnlyRoutes) {
			t.Fatalf("len(sharedKeyOnlyRoutes) = %d, len(auth.SharedKeyOnlyRoutes) = %d, want equal", len(sharedKeyOnlyRoutes), len(auth.SharedKeyOnlyRoutes))
		}
		for surface := range auth.SharedKeyOnlyRoutes {
			if _, ok := sharedKeyOnlyRoutes[surface]; !ok {
				t.Fatalf("root ledger is missing %q from auth.SharedKeyOnlyRoutes", surface)
			}
		}
		if len(pendingRowFilteringRoutes) != len(auth.PendingRowFilteringRoutes) {
			t.Fatalf("len(pendingRowFilteringRoutes) = %d, len(auth.PendingRowFilteringRoutes) = %d, want equal", len(pendingRowFilteringRoutes), len(auth.PendingRowFilteringRoutes))
		}
		for surface := range auth.PendingRowFilteringRoutes {
			if _, ok := pendingRowFilteringRoutes[surface]; !ok {
				t.Fatalf("root ledger is missing %q from auth.PendingRowFilteringRoutes", surface)
			}
		}
	})
	t.Run("classifiers agree", func(t *testing.T) {
		probes := []struct{ method, path string }{
			{"POST", "/api/v0/code/cypher"},
			{"POST", "/api/v0/code/visualize"},
			{"POST", "/api/v0/supply-chain/impact/suppressions"},
			{"GET", "/api/v0/repositories"},
			{"POST", "/api/v0/ask"},
			{"GET", "/sse"},
			{"POST", "/mcp/message"},
		}
		for _, probe := range probes {
			req := surfaceNameToRequest(t, probe.method+" "+probe.path)
			if got, want := IsSharedKeyOnlyRoute(req), auth.IsSharedKeyOnlyRoute(req); got != want {
				t.Errorf("IsSharedKeyOnlyRoute(%s %s) = %v, auth = %v", probe.method, probe.path, got, want)
			}
			if got, want := IsPendingRowFilteringRoute(req), auth.IsPendingRowFilteringRoute(req); got != want {
				t.Errorf("IsPendingRowFilteringRoute(%s %s) = %v, auth = %v", probe.method, probe.path, got, want)
			}
			if got, want := ScopedHTTPRouteSupportsTenantFilter(req), auth.ScopedHTTPRouteSupportsTenantFilter(req); got != want {
				t.Errorf("ScopedHTTPRouteSupportsTenantFilter(%s %s) = %v, auth = %v", probe.method, probe.path, got, want)
			}
			if got, want := scopedRouteNeedsNoCallerGrant(req), auth.ScopedRouteNeedsNoCallerGrant(req); got != want {
				t.Errorf("scopedRouteNeedsNoCallerGrant(%s %s) = %v, auth = %v", probe.method, probe.path, got, want)
			}
		}
	})
	t.Run("denial reasons agree", func(t *testing.T) {
		policy := BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true}
		authPolicy := auth.BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true}
		allScopeSession := AuthContext{Mode: AuthModeBrowserSession, TenantID: "tenant_a", WorkspaceID: "workspace_a", AllScopes: true}
		probes := []string{"/api/v0/code/cypher", "/api/v0/repositories", "/api/v0/capabilities"}
		for _, path := range probes {
			req := surfaceNameToRequest(t, http.MethodPost+" "+path)
			if got, want := browserSessionRouteDenialReason(req, allScopeSession, policy), auth.BrowserSessionRouteDenialReason(req, allScopeSession, authPolicy); got != want {
				t.Errorf("browserSessionRouteDenialReason(%s) = %q, auth = %q", path, got, want)
			}
			scopedCaller := AuthContext{Mode: AuthModeScoped, TenantID: "tenant_a", WorkspaceID: "workspace_a", AllScopes: true}
			if got, want := scopedBearerRouteDenialReason(req, scopedCaller, policy), auth.ScopedBearerRouteDenialReason(req, scopedCaller, authPolicy); got != want {
				t.Errorf("scopedBearerRouteDenialReason(%s) = %q, auth = %q", path, got, want)
			}
		}
	})
	t.Run("policy type is identical", func(t *testing.T) {
		acceptAuthPolicy(BrowserSessionRoutePolicy{})
		acceptRootPolicy(auth.BrowserSessionRoutePolicy{})
	})
}
