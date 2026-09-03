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

			// A restricted token carries a grant the handler binds, so it is
			// decided by allowlist membership alone and the route policy never
			// enters into it. Driving it under the fail-closed policy is the
			// point: the #6450 residual-item-1 fix must not have moved this
			// population.
			wantReason := ""
			if tc.wantStatus == http.StatusForbidden {
				wantReason = scopedRouteNotEnabledReason
			}
			assertBearerFreshnessDeltaRoute(
				t, auth, BrowserSessionRoutePolicy{}, tc.path, tc.wantStatus, wantReason,
			)
		})
	}
}

// TestAllScopeBearerOnFreshnessDeltaRoutesPerGovernanceMode pins what an
// all-scope bearer gets on the two routes this change promotes, per
// ESHU_GOVERNANCE_MODE. "All-scope bearer" is the shape scopedtoken.Registry's
// admin-equivalent entry and an OIDC provider's all-scopes grant set both
// resolve to: AuthModeScoped, AllScopes set, no repository or scope ids.
//
// This table used to assert the opposite. Until #6450's residual item 1
// closed, allowlist membership was the whole bearer gate, so promoting these
// two routes handed such a token a 200 in every deployment -- and because
// RepositoryAccessFilter.Scoped() is false for it, $3 in
// resolveChangedSinceScopeQuery and $8 in listGenerationLifecycleQuery
// short-circuit and the read runs across the whole corpus. The promotion was
// therefore turning a middleware 403 into a cross-tenant read for exactly the
// caller a hosted multi-tenant deployment most needs bounded.
//
// Now the bearer takes the same rule the console session already took: on a
// grant-bound allowlisted route it is admitted only where the operator has
// opted in AND it is bound to one concrete tenant and workspace. The pending
// service route is unchanged and refused in every mode, which is the assertion
// that keeps the promotion honest -- the fix must not have quietly promoted
// the route #6475 is still holding.
func TestAllScopeBearerOnFreshnessDeltaRoutesPerGovernanceMode(t *testing.T) {
	t.Parallel()

	const (
		changedSince   = "/api/v0/freshness/changed-since"
		generations    = "/api/v0/freshness/generations"
		servicePending = "/api/v0/freshness/services/changed-since"
	)

	tenantBound := AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant_a",
		WorkspaceID: "workspace_a",
		AllScopes:   true,
	}
	// A registry entry cannot carry a blank tenant or workspace
	// (scopedtoken.Entry.normalize rejects it) and an OIDC bearer takes both
	// from the provider config, so this shape is defensive: it pins what
	// admission does if some future ScopedTokenResolver hands one over, not a
	// credential an operator can mint today.
	tenantless := AuthContext{Mode: AuthModeScoped, AllScopes: true}

	for _, tc := range []struct {
		name string
		// mode is the raw ESHU_GOVERNANCE_MODE value, read through the same
		// ScopedRoutePolicyForGovernanceMode both commands wire, so this table
		// cannot drift from the deployment posture it claims to describe.
		mode       string
		auth       AuthContext
		path       string
		wantStatus int
		wantReason string
	}{
		{name: "unset mode admits the tenant-bound token on changed-since", mode: "", auth: tenantBound, path: changedSince, wantStatus: http.StatusOK},
		{name: "unset mode admits the tenant-bound token on generations", mode: "", auth: tenantBound, path: generations, wantStatus: http.StatusOK},
		{name: "local_no_policy admits the tenant-bound token on changed-since", mode: "local_no_policy", auth: tenantBound, path: changedSince, wantStatus: http.StatusOK},
		{name: "local_no_policy admits the tenant-bound token on generations", mode: "local_no_policy", auth: tenantBound, path: generations, wantStatus: http.StatusOK},
		{name: "hosted_single_tenant admits the tenant-bound token on changed-since", mode: "hosted_single_tenant", auth: tenantBound, path: changedSince, wantStatus: http.StatusOK},
		{name: "hosted_single_tenant admits the tenant-bound token on generations", mode: "hosted_single_tenant", auth: tenantBound, path: generations, wantStatus: http.StatusOK},

		// The defect this closes, in its reported shape.
		{
			name: "hosted_multi_tenant refuses the tenant-bound token on changed-since",
			mode: "hosted_multi_tenant", auth: tenantBound, path: changedSince,
			wantStatus: http.StatusForbidden, wantReason: scopedRouteAllScopeGrantRequiredReason,
		},
		{
			name: "hosted_multi_tenant refuses the tenant-bound token on generations",
			mode: "hosted_multi_tenant", auth: tenantBound, path: generations,
			wantStatus: http.StatusForbidden, wantReason: scopedRouteAllScopeGrantRequiredReason,
		},
		// An unrecognized mode is fail-closed, so a typo in the deployment's
		// environment cannot silently open the corpus.
		{
			name: "an unrecognized mode is fail-closed on changed-since",
			mode: "hosted-multi-tenant", auth: tenantBound, path: changedSince,
			wantStatus: http.StatusForbidden, wantReason: scopedRouteAllScopeGrantRequiredReason,
		},

		// Tenant-boundness is required on top of the opt-in, not instead of it.
		{
			name: "a tenantless token is refused even where the policy is open",
			mode: "local_no_policy", auth: tenantless, path: changedSince,
			wantStatus: http.StatusForbidden, wantReason: scopedRouteAllScopeGrantRequiredReason,
		},
		{
			name: "a tenantless token is refused under the fail-closed policy too",
			mode: "hosted_multi_tenant", auth: tenantless, path: generations,
			wantStatus: http.StatusForbidden, wantReason: scopedRouteAllScopeGrantRequiredReason,
		},

		// The pending route is off the allowlist, so no policy reaches it and
		// the reason code stays the pre-#6450 one: the route genuinely has no
		// scoped authorization, which is a different thing for an operator to
		// act on than an inert grant.
		{
			name: "the pending service route refuses the tenant-bound token where the policy is open",
			mode: "local_no_policy", auth: tenantBound, path: servicePending,
			wantStatus: http.StatusForbidden, wantReason: scopedRouteNotEnabledReason,
		},
		{
			name: "the pending service route refuses the tenant-bound token under hosted_multi_tenant",
			mode: "hosted_multi_tenant", auth: tenantBound, path: servicePending,
			wantStatus: http.StatusForbidden, wantReason: scopedRouteNotEnabledReason,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			policy := ScopedRoutePolicyForGovernanceMode(GovernanceStatusConfig{Mode: tc.mode})
			assertBearerFreshnessDeltaRoute(t, tc.auth, policy, tc.path, tc.wantStatus, tc.wantReason)
		})
	}
}

// assertBearerFreshnessDeltaRoute drives one bearer read of a freshness delta
// route through the scoped-token middleware under one route policy, and
// asserts the status, whether the next handler ran, and -- on a refusal --
// that the envelope carries the scoped-route permission-denied code and that
// the governance audit recorded the expected reason. The two bearer tables
// above share it so the all-scope rows and the grant-carrying rows are proven
// against the same code path rather than against two hand-copied ones.
//
// The reason assertion is not decoration. Both refusals return the same 403
// with the same body, so the audit row is the only thing that tells an
// operator whether to wire a route up or to narrow a credential, and a
// status-only test cannot see the two drifting into one code.
func assertBearerFreshnessDeltaRoute(
	t *testing.T,
	auth AuthContext,
	policy BrowserSessionRoutePolicy,
	path string,
	wantStatus int,
	wantReason string,
) {
	t.Helper()

	resolver := &fakeScopedTokenResolver{context: auth, ok: true}
	audit := &fakeGovernanceAuditAppender{}
	called := false
	handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy(
		"",
		resolver,
		nil,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
		audit,
		policy,
	)

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
		if len(audit.events) != 0 {
			t.Fatalf("admitted request emitted %d governance-audit event(s), want 0: %#v", len(audit.events), audit.events)
		}
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
	if len(audit.events) != 1 {
		t.Fatalf("len(audit.events) = %d, want 1: %#v", len(audit.events), audit.events)
	}
	if got := audit.events[0].ReasonCode; got != wantReason {
		t.Fatalf("governance-audit reason = %q, want %q", got, wantReason)
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
// ScopedRoutePolicyForGovernanceMode sets AllowTenantBoundAllScopes -- the
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
