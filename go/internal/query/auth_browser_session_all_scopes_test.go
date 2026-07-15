// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareAllScopesTenantBrowserSessionRejectsWholeGraphConsoleRoutes(t *testing.T) {
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
			handler := AuthMiddlewareWithBrowserSessionsAndScopedTokens(
				"shared-token",
				nil,
				resolver,
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					called = true
					_, _ = w.Write([]byte(`{"secret_cross_tenant_data":true}`))
				}),
			)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
			if browserSessionRequiresCSRF(tc.method) {
				req.Header.Set(BrowserSessionCSRFHeaderName, "csrf-secret")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assertWholeGraphRouteDenied(t, rec, called)
			if got, want := resolver.requireCSRF, browserSessionRequiresCSRF(tc.method); got != want {
				t.Fatalf("requireCSRF = %t, want %t", got, want)
			}
		})
	}
}

func TestAuthMiddlewareRestrictedCredentialsCannotEnterWholeGraphConsoleRoutes(t *testing.T) {
	t.Parallel()

	routes := []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/v0/code/dead-code"},
		{method: http.MethodGet, path: "/api/v0/graph/entities"},
		{method: http.MethodGet, path: "/api/v0/ecosystem/overview"},
		{method: http.MethodPost, path: "/api/v0/visualizations/derive"},
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
			handler := AuthMiddlewareWithBrowserSessionsAndScopedTokens(
				"shared-token",
				nil,
				resolver,
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					called = true
					_, _ = w.Write([]byte(`{"secret_cross_tenant_data":true}`))
				}),
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
			handler := AuthMiddlewareWithScopedTokens(
				"shared-token",
				resolver,
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					called = true
					_, _ = w.Write([]byte(`{"secret_cross_tenant_data":true}`))
				}),
			)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assertWholeGraphRouteDenied(t, rec, called)
		})
	}
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
