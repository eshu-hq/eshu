// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAuthMiddlewareAllScopesBrowserSessionSplitAcrossLedger drives every
// scoped-route allowlist entry through the production middleware constructor
// under all six caller shapes #6450 has to keep apart. The point is coverage
// of the whole ledger, not of a hand-picked sample: the defect was invisible
// precisely because the routes that had to keep working (the /api/v0/auth/
// admin console) and the routes that had to stop working (everything
// grant-filtered) sat in the same allowlist with nothing distinguishing them.
//
// Only shapes (b) and (f) -- an all-scope session under the fail-closed
// policy, and a malformed tenantless all-scope session even under the
// permissive one -- vary by class. The grant-bearing bearer, the restricted
// session, the shared key, and the all-scope session under the permissive
// policy reach every entry, which is what keeps this a split rather than a
// blanket refusal.
func TestAuthMiddlewareAllScopesBrowserSessionSplitAcrossLedger(t *testing.T) {
	t.Parallel()

	for _, shape := range allScopesSplitShapes() {
		shape := shape
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()

			for surfaceName, class := range scopedTokenAdvertisedRoutes {
				surfaceName, class := surfaceName, class
				t.Run(surfaceName, func(t *testing.T) {
					t.Parallel()
					runAllScopesSplitCase(t, shape, surfaceName, class)
				})
			}
		})
	}
}

// TestAuthMiddlewareAllScopesBrowserSessionSplitOnUnledgeredTransportRoutes
// covers the two allowlisted routes with no ledger entry and therefore no
// class: the MCP transport paths. They take the fail-closed default, so an
// all-scope session reaches them only under the permissive policy. Without
// this, the split table would silently skip the only allowlisted routes whose
// class is implicit rather than written down.
func TestAuthMiddlewareAllScopesBrowserSessionSplitOnUnledgeredTransportRoutes(t *testing.T) {
	t.Parallel()

	shapes := map[string]allScopesSplitShape{}
	for _, shape := range allScopesSplitShapes() {
		shapes[shape.name] = shape
	}

	for _, shapeName := range []string{
		"b_all_scope_session_fail_closed_policy",
		"c_all_scope_session_permissive_policy",
	} {
		shape, ok := shapes[shapeName]
		if !ok {
			t.Fatalf("caller shape %q is gone from allScopesSplitShapes", shapeName)
		}
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()

			for _, surfaceName := range []string{"GET /sse", "POST /mcp/message"} {
				surfaceName := surfaceName
				t.Run(surfaceName, func(t *testing.T) {
					t.Parallel()
					// scopedRouteGrantBound is the zero value an unledgered
					// route effectively carries, which is exactly the
					// fail-closed behaviour being asserted.
					runAllScopesSplitCase(t, shape, surfaceName, scopedRouteGrantBound)
				})
			}
		})
	}
}

// TestAuthMiddlewareAllScopesBrowserSessionRefusedOnGrantBoundRouteUnderFailClosedPolicy
// is the named regression test for issue #6450 in its reported shape. Before
// the split, browserSessionRouteAllowed returned true as soon as
// scopedHTTPRouteSupportsTenantFilter(r) did, so an all-scope console session
// reached GET /api/v0/repositories in a hosted_multi_tenant deployment that
// had deliberately left BrowserSessionRoutePolicy at its fail-closed zero
// value. RepositoryHandler's grant predicate cannot save it: an all-scope
// caller's RepositoryAccessFilterFromContext is not Scoped(), and no
// data-plane table carries a tenant column, so the read crossed tenants.
//
// The three cases together are the whole fix: the all-scope session is
// refused under the fail-closed policy, the same session is admitted when the
// operator explicitly opts in, and a restricted session -- whose grant the
// handler can actually bind -- is unaffected.
func TestAuthMiddlewareAllScopesBrowserSessionRefusedOnGrantBoundRouteUnderFailClosedPolicy(t *testing.T) {
	t.Parallel()

	const path = "/api/v0/repositories"

	allScopes := AuthContext{
		Mode:        AuthModeBrowserSession,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		AllScopes:   true,
	}
	restricted := AuthContext{
		Mode:                 AuthModeBrowserSession,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
	}

	for _, tc := range []struct {
		name       string
		session    AuthContext
		policy     BrowserSessionRoutePolicy
		wantStatus int
		wantCalled bool
	}{
		{
			name:       "all scopes under fail-closed policy is refused",
			session:    allScopes,
			policy:     BrowserSessionRoutePolicy{},
			wantStatus: http.StatusForbidden,
			wantCalled: false,
		},
		{
			name:       "all scopes under permissive policy is admitted",
			session:    allScopes,
			policy:     BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "restricted session is unaffected",
			session:    restricted,
			policy:     BrowserSessionRoutePolicy{},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			called := false
			handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditRoutePolicyAndEnforcement(
				splitTestSharedToken,
				nil,
				&fakeBrowserSessionResolver{context: tc.session, ok: true},
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					called = true
					_, _ = w.Write([]byte(splitTestHandlerPayload))
				}),
				nil,
				tc.policy,
				true,
			)

			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if called != tc.wantCalled {
				t.Fatalf("handler called = %t, want %t; status = %d, body = %s", called, tc.wantCalled, rec.Code, rec.Body.String())
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if !tc.wantCalled && rec.Body.String() == splitTestHandlerPayload {
				t.Fatalf("denied response exposed handler data: %s", rec.Body.String())
			}
		})
	}
}
