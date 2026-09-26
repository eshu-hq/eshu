// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// TestScopedTokenReachesFreshnessDeltaRoutes pins which freshness delta reads
// a scoped token may reach. The pair keyed on repository/scope rows was
// promoted by #5167's freshness workstream; the service lineage read joined
// them once #6475 gave every lineage row the scope_id of the ingestion scope
// that wrote it, so its grant binds in SQL on that column
// (resolveServiceChangedSinceScopeQuery) exactly as the pair's does.
func TestScopedTokenReachesFreshnessDeltaRoutes(t *testing.T) {
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
			name:       "service_changed_since_promoted",
			path:       "/api/v0/freshness/services/changed-since",
			wantStatus: http.StatusOK,
		},
	}

	authCtx := AuthContext{
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
				t, authCtx, BrowserSessionRoutePolicy{}, tc.path, tc.wantStatus, wantReason,
			)
		})
	}
}

// TestAllScopeBearerOnFreshnessDeltaRoutesPerGovernanceMode pins what an
// all-scope bearer gets on the promoted freshness delta routes, per
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
// opted in AND it is bound to one concrete tenant and workspace. The service
// route, promoted by #6475 part B, takes the same rule.
func TestAllScopeBearerOnFreshnessDeltaRoutesPerGovernanceMode(t *testing.T) {
	t.Parallel()

	const (
		changedSince   = "/api/v0/freshness/changed-since"
		generations    = "/api/v0/freshness/generations"
		serviceChanges = "/api/v0/freshness/services/changed-since"
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

		// The service route was promoted by #6475 part B, so it now takes the
		// same all-scope rule as the pair: admitted where the operator opted
		// in, refused under hosted_multi_tenant with the all-scope reason.
		{
			name: "local_no_policy admits the tenant-bound token on service changed-since",
			mode: "local_no_policy", auth: tenantBound, path: serviceChanges,
			wantStatus: http.StatusOK,
		},
		{
			name: "hosted_multi_tenant refuses the tenant-bound token on service changed-since",
			mode: "hosted_multi_tenant", auth: tenantBound, path: serviceChanges,
			wantStatus: http.StatusForbidden, wantReason: scopedRouteAllScopeGrantRequiredReason,
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
	authCtx AuthContext,
	policy BrowserSessionRoutePolicy,
	path string,
	wantStatus int,
	wantReason string,
) {
	t.Helper()

	resolver := &fakeScopedTokenResolver{context: authCtx, ok: true}
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

// TestServiceChangedSinceLeftPendingLedger asserts the ledger side of the #6475
// part B promotion directly: the route is off pendingRowFilteringRoutes,
// matched by the scoped-token allowlist, and advertised as grant-bound, like
// its two freshness siblings.
func TestServiceChangedSinceLeftPendingLedger(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/freshness/changed-since",
		"/api/v0/freshness/generations",
		"/api/v0/freshness/services/changed-since",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if IsPendingRowFilteringRoute(req) {
			t.Fatalf("IsPendingRowFilteringRoute(%s) = true, want false", path)
		}
		if !ScopedHTTPRouteSupportsTenantFilter(req) {
			t.Fatalf("ScopedHTTPRouteSupportsTenantFilter(%s) = false, want true", path)
		}
		if got, ok := scopedTokenAdvertisedRoutes["GET "+path]; !ok || got != scopedRouteGrantBound {
			t.Fatalf("scopedTokenAdvertisedRoutes[GET %s] = %v (present %t), want scopedRouteGrantBound", path, got, ok)
		}
	}
}

// TestServiceChangedSinceBrowserSessionAdmissionPerPolicy pins which BROWSER
// SESSION shapes the promoted service route admits, per BrowserSessionRoutePolicy
// mode. As a grant-bound allowlisted route it admits a restricted session in
// every mode -- the lineage SQL binds its grant -- and admits a tenant-bound
// all-scope console session only where the policy opts in
// (AllowTenantBoundAllScopes: local_no_policy, hosted_single_tenant, unset).
// A tenantless all-scope session is refused everywhere.
func TestServiceChangedSinceBrowserSessionAdmissionPerPolicy(t *testing.T) {
	t.Parallel()

	const servicePath = "/api/v0/freshness/services/changed-since"

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
			name: "restricted_session_admitted_where_the_policy_is_open",
			auth: AuthContext{
				Mode:                 AuthModeBrowserSession,
				TenantID:             "tenant_a",
				WorkspaceID:          "workspace_a",
				AllowedRepositoryIDs: []string{"repo_a"},
			},
			policy:     BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
			wantStatus: http.StatusOK,
		},
		{
			name: "restricted_session_admitted_under_hosted_multi_tenant",
			auth: AuthContext{
				Mode:                 AuthModeBrowserSession,
				TenantID:             "tenant_a",
				WorkspaceID:          "workspace_a",
				AllowedRepositoryIDs: []string{"repo_a"},
			},
			policy:     BrowserSessionRoutePolicy{},
			wantStatus: http.StatusOK,
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

			req := httptest.NewRequest(http.MethodGet, servicePath, nil)
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

// TestAuthMiddlewareWithScopedTokensAllowsRepositoryFreshnessRoute is the PR
// #5150 review regression (codex + carried-forward P1): the two scoped tests
// above mount the handler on a bare http.NewServeMux(), which bypasses
// AuthMiddlewareWithScopedTokens entirely and encoded a false green --
// GET /api/v0/repositories/{repo_id}/freshness was never added to
// scopedHTTPRouteSupportsTenantFilter (auth_scoped_routes.go), so a real
// scoped-token or browser-session caller got 403 from the middleware before
// getRepositoryFreshness's grant filtering (and the promised 404 parity)
// could ever run. This test routes through the real middleware, matching the
// #5137 pattern in auth_ingester_status_test.go
// (TestAuthMiddlewareWithScopedTokensAllowsIngesterStatusRoutes).
func TestAuthMiddlewareWithScopedTokensAllowsRepositoryFreshnessRoute(t *testing.T) {
	t.Parallel()

	newMiddlewareWrappedHandler := func(allowedRepositoryIDs []string) http.Handler {
		reader := &testutil.FakeRepositoryFreshnessReader{Snapshot: testutil.FullyBuiltRepositoryFreshnessSnapshot()}
		// Mirrors repositoryFreshnessTestHandler in internal/query/repository:
		// that helper is unexported to its package, so this root middleware
		// test inlines the same handler shape with root's fakeRepoGraphReader.
		handler := &RepositoryHandler{
			Neo4j: fakeRepoGraphReader{
				runSingleByMatch: map[string]map[string]any{
					"MATCH (r:Repository {id: $repo_id})": testutil.RepositoryStatsGraphRow(),
				},
			},
			Content:   content.FakePortContentStore{Repositories: []querycontract.RepositoryCatalogEntry{testutil.RepositoryStatsCatalogEntry()}},
			Freshness: reader,
		}
		mux := http.NewServeMux()
		handler.Mount(mux)
		resolver := &testutil.FakeScopedTokenResolver{
			Context: auth.AuthContext{
				Mode:                 auth.AuthModeScoped,
				TenantID:             "tenant-a",
				WorkspaceID:          "workspace-a",
				SubjectClass:         "team",
				SubjectIDHash:        "sha256:team-a",
				PolicyRevisionHash:   "sha256:policy",
				AllowedRepositoryIDs: allowedRepositoryIDs,
			},
			OK: true,
		}
		return AuthMiddlewareWithScopedTokens("", resolver, mux)
	}

	t.Run("grant reaches the handler", func(t *testing.T) {
		t.Parallel()

		middleware := newMiddlewareWrappedHandler([]string{"repo-1"})
		req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/freshness", nil)
		req.Header.Set("Authorization", "Bearer [REDACTED]")
		w := httptest.NewRecorder()
		middleware.ServeHTTP(w, req)

		if got, want := w.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d (middleware must not 403 a granted scoped caller); body = %s", got, want, w.Body.String())
		}
		resp := testutil.DecodeResponseBody(t, w)
		if got, want := resp["scoped"], true; got != want {
			t.Fatalf("scoped = %#v, want %#v", got, want)
		}
	})

	t.Run("no grant is a 404, not a 403", func(t *testing.T) {
		t.Parallel()

		middleware := newMiddlewareWrappedHandler([]string{"repo-other"})
		req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/freshness", nil)
		req.Header.Set("Authorization", "Bearer [REDACTED]")
		w := httptest.NewRecorder()
		middleware.ServeHTTP(w, req)

		if got, want := w.Code, http.StatusNotFound; got != want {
			t.Fatalf("status = %d, want %d (grant filtering, not middleware 403); body = %s", got, want, w.Body.String())
		}
	})
}
