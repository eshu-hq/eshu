// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestScopedTokenReachesFreshnessDeltaPairOnly pins which freshness delta
// reads a scoped token may reach after #5167's freshness workstream. The pair
// keyed on repository/scope rows is promoted; the service lineage read is not,
// because service_materialization_generations carries no column that names the
// tenant its rows belong to (#6475). Promoting it while that is true would let
// one tenant read another's lineage whenever the correlation the handler fence
// probes has aged out of the correlating scope's active generation, which is
// exactly the "a promoted route never turns a 403 into a cross-tenant read"
// contract in pendingRowFilteringRoutes' header.
//
// The service route's handler fence (serviceChangedSinceGrantAdmits) is in the
// tree and tested, so this assertion is about the middleware ledger, not about
// the fence: the fence is the first half of the promotion and #6475 is the
// second.
func TestScopedTokenReachesFreshnessDeltaPairOnly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "changed_since_promoted",
			path:       "/api/v0/freshness/changed-since",
			wantStatus: http.StatusOK,
		},
		{
			name:       "generations_promoted",
			path:       "/api/v0/freshness/generations",
			wantStatus: http.StatusOK,
		},
		{
			name:       "service_changed_since_still_pending",
			path:       "/api/v0/freshness/services/changed-since",
			wantStatus: http.StatusForbidden,
		},
	}

	auth := AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant_a",
		WorkspaceID:          "workspace_a",
		AllowedRepositoryIDs: []string{"repo_a"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertBearerFreshnessDeltaRoute(t, auth, tc.path, tc.wantStatus)
		})
	}
}

// TestAllScopeBearerTokenReachesFreshnessDeltaPairOnly pins the bearer shape
// the route's four caller-facing surfaces name when they say scoped tokens are
// refused "all-scope bearer tokens included": a token whose grant set carries
// AllScopes and no repository or scope ids, which is what
// scopedtoken.Registry's admin-equivalent entry and an OIDC provider's
// all-scopes grant set both resolve to.
//
// Neither resolver varies Mode with AllScopes -- both build the context with
// AuthModeScoped -- and the middleware's scoped branch keys on Mode alone, so
// the pending service route refuses this token in every deployment while the
// two promoted pair routes hand it to the handler.
//
// That asymmetry is the assertion. The tenant-bound all-scope BROWSER SESSION
// in the table below IS admitted on the same pending route wherever the policy
// sets AllowTenantBoundAllScopes, so "all-scope" alone does not decide the
// route: the credential kind does. #6450's residual rests on that split, which
// is why it gets a case of its own rather than being read off the
// browser-session table.
func TestAllScopeBearerTokenReachesFreshnessDeltaPairOnly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "changed_since_promoted",
			path:       "/api/v0/freshness/changed-since",
			wantStatus: http.StatusOK,
		},
		{
			name:       "generations_promoted",
			path:       "/api/v0/freshness/generations",
			wantStatus: http.StatusOK,
		},
		{
			name:       "service_changed_since_still_pending",
			path:       "/api/v0/freshness/services/changed-since",
			wantStatus: http.StatusForbidden,
		},
	}

	auth := AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant_a",
		WorkspaceID: "workspace_a",
		AllScopes:   true,
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertBearerFreshnessDeltaRoute(t, auth, tc.path, tc.wantStatus)
		})
	}
}

// assertBearerFreshnessDeltaRoute drives one bearer read of a freshness delta
// route through the scoped-token middleware and asserts the status, whether
// the next handler ran, and -- on a refusal -- that the envelope carries the
// scoped-route permission-denied code. The two bearer tables above share it so
// the all-scope row and the grant-carrying row are proven against the same
// code path rather than against two hand-copied ones.
func assertBearerFreshnessDeltaRoute(t *testing.T, auth AuthContext, path string, wantStatus int) {
	t.Helper()

	resolver := &fakeScopedTokenResolver{context: auth, ok: true}
	called := false
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.Header.Set("Authorization", "Bearer scoped-token")
	req.Header.Set("X-Correlation-ID", "corr-freshness-delta")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Code; got != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", got, wantStatus, rec.Body.String())
	}
	if want := wantStatus == http.StatusOK; called != want {
		t.Fatalf("next handler called = %t, want %t", called, want)
	}
	if wantStatus != http.StatusForbidden {
		return
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Error == nil {
		t.Fatalf("envelope.Error = nil, want scoped-route denial; body = %s", rec.Body.String())
	}
	if got, want := envelope.Error.Code, ErrorCodePermissionDenied; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
}

// TestServiceChangedSinceStaysOnPendingLedger asserts the ledger side of the
// same withdrawal directly, so a future contributor who allowlists the route
// without the #6475 schema change fails here as well as in the middleware
// table above.
func TestServiceChangedSinceStaysOnPendingLedger(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/v0/freshness/services/changed-since", nil)
	if !IsPendingRowFilteringRoute(req) {
		t.Fatal("IsPendingRowFilteringRoute() = false, want true until #6475 names the tenant on lineage rows")
	}
	if ScopedHTTPRouteSupportsTenantFilter(req) {
		t.Fatal("ScopedHTTPRouteSupportsTenantFilter() = true, want false while the route is pending")
	}
	if _, ok := scopedTokenAdvertisedRoutes["GET /api/v0/freshness/services/changed-since"]; ok {
		t.Fatal("service changed-since is advertised in scopedTokenAdvertisedRoutes, want absent while pending")
	}
	for _, promoted := range []string{"/api/v0/freshness/changed-since", "/api/v0/freshness/generations"} {
		promotedReq := httptest.NewRequest(http.MethodGet, promoted, nil)
		if !ScopedHTTPRouteSupportsTenantFilter(promotedReq) {
			t.Fatalf("ScopedHTTPRouteSupportsTenantFilter(%s) = false, want true", promoted)
		}
		if IsPendingRowFilteringRoute(promotedReq) {
			t.Fatalf("IsPendingRowFilteringRoute(%s) = true, want false", promoted)
		}
	}
}

// TestServiceChangedSincePendingRouteAdmitsOnlyTheAllScopeConsoleSession pins
// which BROWSER SESSION shapes the pending service route actually refuses, per
// BrowserSessionRoutePolicy mode. The route is absent from
// scopedTokenAdvertisedRoutes, so browserSessionRouteDenialReason decides it on
// the same branch it uses for every route outside that allowlist: a session
// that is all-scope AND bound to one tenant and workspace is admitted wherever
// cmd/api's browserSessionRoutePolicy sets AllowTenantBoundAllScopes -- the
// local_no_policy, hosted_single_tenant, and unset modes -- and every other
// shape is refused with a 403.
//
// The middleware-refuses-everyone reading is what the route's contract prose
// used to claim, and it is wrong on those three modes: the admitted session
// reaches serviceChangedSinceGrantAdmits, whose first branch returns true for
// an unscoped caller, so it reads the lineage. That is the same whole-graph
// posture the policy grants on every other non-allowlisted route, not a hole
// specific to this one, but the prose has to say so.
func TestServiceChangedSincePendingRouteAdmitsOnlyTheAllScopeConsoleSession(t *testing.T) {
	t.Parallel()

	const pendingPath = "/api/v0/freshness/services/changed-since"

	cases := []struct {
		name       string
		auth       AuthContext
		policy     BrowserSessionRoutePolicy
		wantStatus int
	}{
		{
			name: "all_scope_console_session_admitted_under_local_or_single_tenant",
			auth: AuthContext{
				Mode:        AuthModeBrowserSession,
				TenantID:    "tenant_a",
				WorkspaceID: "workspace_a",
				AllScopes:   true,
			},
			policy:     BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
			wantStatus: http.StatusOK,
		},
		{
			name: "all_scope_console_session_refused_under_hosted_multi_tenant",
			auth: AuthContext{
				Mode:        AuthModeBrowserSession,
				TenantID:    "tenant_a",
				WorkspaceID: "workspace_a",
				AllScopes:   true,
			},
			policy:     BrowserSessionRoutePolicy{},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "restricted_session_refused_even_where_the_policy_is_open",
			auth: AuthContext{
				Mode:                 AuthModeBrowserSession,
				TenantID:             "tenant_a",
				WorkspaceID:          "workspace_a",
				AllowedRepositoryIDs: []string{"repo_a"},
			},
			policy:     BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "tenantless_all_scope_session_refused_even_where_the_policy_is_open",
			auth: AuthContext{
				Mode:      AuthModeBrowserSession,
				AllScopes: true,
			},
			policy:     BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeBrowserSessionResolver{context: tc.auth, ok: true}
			called := false
			handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy(
				"shared-token",
				nil,
				resolver,
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					called = true
					w.WriteHeader(http.StatusOK)
				}),
				nil,
				tc.policy,
			)

			req := httptest.NewRequest(http.MethodGet, pendingPath, nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			req.Header.Set("X-Correlation-ID", "corr-service-changed-since-session")
			req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got := rec.Code; got != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", got, tc.wantStatus, rec.Body.String())
			}
			if want := tc.wantStatus == http.StatusOK; called != want {
				t.Fatalf("next handler called = %t, want %t", called, want)
			}
		})
	}
}
