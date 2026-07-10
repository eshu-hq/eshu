// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAuthMiddlewareWithBrowserSessionsAllowsSignInPolicyAdminRoutes reproduces
// issue #5004: a real admin authenticated by a browser-session cookie (the
// console's only auth mode — apps/console/src/api/client.ts) was rejected with
// the scoped-authorization 403 on both GET and PATCH
// /api/v0/auth/admin/sign-in-policy before the handler's own AllScopes check
// ever ran, because scopedHTTPRouteSupportsTenantFilter did not allowlist
// these routes (scopedAuthAdminReadRoute omitted the GET, and
// scopedAuthAdminMutationRoute had no PATCH case at all). This drives real
// HTTP requests carrying the session cookie through the actual
// AuthMiddlewareWithBrowserSessionsAndScopedTokens middleware — not a direct
// ContextWithAuthContext injection — so the repro exercises the exact
// production code path a real admin hits, and the whole #4968 Sign-in Policy
// admin panel becomes reachable again.
func TestAuthMiddlewareWithBrowserSessionsAllowsSignInPolicyAdminRoutes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"admin read", http.MethodGet, "/api/v0/auth/admin/sign-in-policy", ""},
		{"admin update", http.MethodPatch, "/api/v0/auth/admin/sign-in-policy", `{}`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			readHandler := &SignInPolicyReadHandler{
				Store: &fakeSignInPolicyReadStore{policy: SignInPolicy{TenantID: "tenant_a"}},
			}
			mutationHandler := &SignInPolicyMutationHandler{
				Store: &fakeSignInPolicyMutationStore{result: SignInPolicy{TenantID: "tenant_a"}},
				Audit: &fakeSignInPolicyAudit{},
			}
			mux := http.NewServeMux()
			readHandler.Mount(mux)
			mutationHandler.Mount(mux)

			resolver := &fakeBrowserSessionResolver{
				context: AuthContext{
					Mode:        AuthModeBrowserSession,
					TenantID:    "tenant_a",
					WorkspaceID: "workspace_a",
					AllScopes:   true,
				},
				ok: true,
			}
			handler := AuthMiddlewareWithBrowserSessionsAndScopedTokens("", nil, resolver, mux)

			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
			if browserSessionRequiresCSRF(tc.method) {
				req.Header.Set(BrowserSessionCSRFHeaderName, "csrf-secret")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code == http.StatusForbidden {
				t.Fatalf(
					"%s %s rejected before reaching the handler (scoped-route allowlist gap #5004): status=%d body=%s",
					tc.method, tc.path, rec.Code, rec.Body.String(),
				)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s status = %d, want %d: %s", tc.method, tc.path, rec.Code, http.StatusOK, rec.Body.String())
			}
		})
	}
}
