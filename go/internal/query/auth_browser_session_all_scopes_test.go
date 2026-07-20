// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareAllScopesTenantBrowserSessionAllowsWholeGraphConsoleRoutes(t *testing.T) {
	t.Parallel()

	routes := []struct {
		name   string
		method string
		path   string
	}{
		{name: "dead code", method: http.MethodPost, path: "/api/v0/code/dead-code"},
		{name: "call graph", method: http.MethodPost, path: "/api/v0/code/call-graph/metrics"},
		{name: "graph entities", method: http.MethodGet, path: "/api/v0/graph/entities"},
		{name: "ecosystem overview", method: http.MethodGet, path: "/api/v0/ecosystem/overview"},
		{name: "changed since", method: http.MethodGet, path: "/api/v0/freshness/changed-since"},
		{name: "entity map", method: http.MethodPost, path: "/api/v0/impact/entity-map"},
		{name: "visualization", method: http.MethodPost, path: "/api/v0/visualizations/derive"},
	}

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &fakeBrowserSessionResolver{
				context: AuthContext{
					Mode:        AuthModeBrowserSession,
					TenantID:    "tenant-a",
					WorkspaceID: "workspace-a",
					AllScopes:   true,
				},
				ok: true,
			}
			called := false
			handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy(
				"shared-token",
				nil,
				resolver,
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					auth, found := AuthContextFromContext(r.Context())
					if !found {
						t.Fatal("next handler missing browser-session auth context")
					}
					if auth.Mode != AuthModeBrowserSession || !auth.AllScopes ||
						auth.TenantID != "tenant-a" || auth.WorkspaceID != "workspace-a" {
						t.Fatalf("next handler auth = %#v, want tenant-bound all-scopes browser session", auth)
					}
					w.WriteHeader(http.StatusOK)
				}),
				nil,
				BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
			)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
			if browserSessionRequiresCSRF(tc.method) {
				req.Header.Set(BrowserSessionCSRFHeaderName, "csrf-secret")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if !called {
				t.Fatal("next handler not called for tenant-bound all-scopes browser session")
			}
			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if got, want := resolver.requireCSRF, browserSessionRequiresCSRF(tc.method); got != want {
				t.Fatalf("requireCSRF = %t, want %t", got, want)
			}
		})
	}
}

func TestAuthMiddlewareAllScopesTenantBrowserSessionDefaultsWholeGraphConsoleRoutesToDenied(t *testing.T) {
	t.Parallel()

	called := false
	resolver := &fakeBrowserSessionResolver{
		context: AuthContext{
			Mode:        AuthModeBrowserSession,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
			AllScopes:   true,
		},
		ok: true,
	}
	handler := AuthMiddlewareWithBrowserSessionsAndScopedTokens(
		"shared-token",
		nil,
		resolver,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			_, _ = w.Write([]byte(`{"secret_cross_tenant_data":true}`))
		}),
	)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities", nil)
	req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertWholeGraphRouteDenied(t, rec, called)
}

func TestAuthMiddlewareTenantlessAllScopesBrowserSessionCannotEnterWholeGraphConsoleRoutes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		tenantID    string
		workspaceID string
	}{
		{name: "missing tenant", workspaceID: "workspace-a"},
		{name: "missing workspace", tenantID: "tenant-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			resolver := &fakeBrowserSessionResolver{
				context: AuthContext{
					Mode:        AuthModeBrowserSession,
					TenantID:    tc.tenantID,
					WorkspaceID: tc.workspaceID,
					AllScopes:   true,
				},
				ok: true,
			}
			handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy(
				"shared-token",
				nil,
				resolver,
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					called = true
					_, _ = w.Write([]byte(`{"secret_cross_tenant_data":true}`))
				}),
				nil,
				BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
			)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities", nil)
			req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assertWholeGraphRouteDenied(t, rec, called)
		})
	}
}

func TestAuthMiddlewareRestrictedCredentialsCannotEnterWholeGraphConsoleRoutes(t *testing.T) {
	t.Parallel()

	// POST /api/v0/visualizations/derive is deliberately absent from this
	// table (#5167 task 4): VisualizationHandler holds no graph, content, or
	// store reference (visualization_packet_handler.go) -- it only reshapes
	// the caller-supplied source_response the restricted caller already
	// possesses, so it was moved into scopedHTTPRouteSupportsTenantFilter and
	// a restricted credential is now expected to reach it, not be denied.
	//
	// GET /api/v0/ecosystem/overview is also deliberately absent (#5167 F-6
	// W6 cloud/aws family workstream): getEcosystemOverview
	// (infra_ecosystem_overview.go) now restricts every count to entities
	// reachable from the caller's granted repositories via
	// DEFINES/INSTANCE_OF/RUNS_ON (runEcosystemOverviewCounts), so a
	// restricted credential is expected to reach it and receive grant-bound
	// counts, not the whole-graph total. See
	// TestAuthMiddlewareRestrictedCredentialsReachEcosystemOverviewRoute.
	routes := []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/v0/code/dead-code"},
		{method: http.MethodGet, path: "/api/v0/graph/entities"},
	}

	for _, tc := range routes {
		t.Run("browser "+tc.path, func(t *testing.T) {
			called := false
			resolver := &fakeBrowserSessionResolver{
				context: AuthContext{
					Mode:                 AuthModeBrowserSession,
					TenantID:             "tenant-a",
					WorkspaceID:          "workspace-a",
					AllowedRepositoryIDs: []string{"repo-a"},
				},
				ok: true,
			}
			handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy(
				"shared-token",
				nil,
				resolver,
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					called = true
					_, _ = w.Write([]byte(`{"secret_cross_tenant_data":true}`))
				}),
				nil,
				BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
			)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
			if browserSessionRequiresCSRF(tc.method) {
				req.Header.Set(BrowserSessionCSRFHeaderName, "csrf-secret")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assertWholeGraphRouteDenied(t, rec, called)
		})

		t.Run("bearer "+tc.path, func(t *testing.T) {
			called := false
			resolver := &fakeScopedTokenResolver{
				context: AuthContext{
					Mode:                 AuthModeScoped,
					TenantID:             "tenant-a",
					WorkspaceID:          "workspace-a",
					AllScopes:            true,
					AllowedRepositoryIDs: []string{"repo-a"},
				},
				ok: true,
			}
			handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy(
				"shared-token",
				resolver,
				nil,
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					called = true
					_, _ = w.Write([]byte(`{"secret_cross_tenant_data":true}`))
				}),
				nil,
				BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
			)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assertWholeGraphRouteDenied(t, rec, called)
		})
	}
}

// TestAuthMiddlewareRestrictedCredentialsReachVisualizationDeriveRoute is the
// #5167 task 4 counterpart of
// TestAuthMiddlewareRestrictedCredentialsCannotEnterWholeGraphConsoleRoutes:
// it proves a restricted (non-all-scopes, single-repository-grant) caller now
// reaches POST /api/v0/visualizations/derive under the real production
// middleware (AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy),
// for both a browser-session cookie and a scoped bearer token, instead of the
// 403 every other whole-graph console route still returns to the same
// credential shape.
func TestAuthMiddlewareRestrictedCredentialsReachVisualizationDeriveRoute(t *testing.T) {
	t.Parallel()

	const path = "/api/v0/visualizations/derive"

	t.Run("browser session", func(t *testing.T) {
		t.Parallel()

		called := false
		resolver := &fakeBrowserSessionResolver{
			context: AuthContext{
				Mode:                 AuthModeBrowserSession,
				TenantID:             "tenant-a",
				WorkspaceID:          "workspace-a",
				AllowedRepositoryIDs: []string{"repo-a"},
			},
			ok: true,
		}
		handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy(
			"shared-token",
			nil,
			resolver,
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}),
			nil,
			BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
		)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
		req.Header.Set(BrowserSessionCSRFHeaderName, "csrf-secret")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if !called {
			t.Fatalf("next handler not called for a restricted browser session; status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
	})

	t.Run("scoped bearer", func(t *testing.T) {
		t.Parallel()

		called := false
		resolver := &fakeScopedTokenResolver{
			context: AuthContext{
				Mode:                 AuthModeScoped,
				TenantID:             "tenant-a",
				WorkspaceID:          "workspace-a",
				AllowedRepositoryIDs: []string{"repo-a"},
			},
			ok: true,
		}
		handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy(
			"shared-token",
			resolver,
			nil,
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}),
			nil,
			BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
		)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Authorization", "Bearer scoped-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if !called {
			t.Fatalf("next handler not called for a restricted scoped bearer; status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
	})
}

// TestAuthMiddlewareRestrictedCredentialsReachEcosystemOverviewRoute is the
// #5167 F-6 W6 cloud/aws family counterpart of
// TestAuthMiddlewareRestrictedCredentialsReachVisualizationDeriveRoute: it
// proves a restricted (non-all-scopes, single-repository-grant) caller now
// reaches GET /api/v0/ecosystem/overview under the real production
// middleware (AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy),
// for both a browser-session cookie and a scoped bearer token, instead of the
// 403 every other whole-graph console route still returns to the same
// credential shape. getEcosystemOverview restricts the counts to the
// caller's grant (runEcosystemOverviewCounts), so reaching the handler no
// longer discloses whole-graph totals.
func TestAuthMiddlewareRestrictedCredentialsReachEcosystemOverviewRoute(t *testing.T) {
	t.Parallel()

	const path = "/api/v0/ecosystem/overview"

	t.Run("browser session", func(t *testing.T) {
		t.Parallel()

		called := false
		resolver := &fakeBrowserSessionResolver{
			context: AuthContext{
				Mode:                 AuthModeBrowserSession,
				TenantID:             "tenant-a",
				WorkspaceID:          "workspace-a",
				AllowedRepositoryIDs: []string{"repo-a"},
			},
			ok: true,
		}
		handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy(
			"shared-token",
			nil,
			resolver,
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}),
			nil,
			BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
		)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if !called {
			t.Fatalf("next handler not called for a restricted browser session; status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
	})

	t.Run("scoped bearer", func(t *testing.T) {
		t.Parallel()

		called := false
		resolver := &fakeScopedTokenResolver{
			context: AuthContext{
				Mode:                 AuthModeScoped,
				TenantID:             "tenant-a",
				WorkspaceID:          "workspace-a",
				AllowedRepositoryIDs: []string{"repo-a"},
			},
			ok: true,
		}
		handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy(
			"shared-token",
			resolver,
			nil,
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}),
			nil,
			BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
		)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer scoped-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if !called {
			t.Fatalf("next handler not called for a restricted scoped bearer; status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
	})
}

func assertWholeGraphRouteDenied(t *testing.T, rec *httptest.ResponseRecorder, handlerCalled bool) {
	t.Helper()
	if handlerCalled {
		t.Fatal("next handler called for restricted credential")
	}
	if got, want := rec.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if rec.Body.String() == `{"secret_cross_tenant_data":true}` {
		t.Fatalf("denied response exposed handler data: %s", rec.Body.String())
	}
}
