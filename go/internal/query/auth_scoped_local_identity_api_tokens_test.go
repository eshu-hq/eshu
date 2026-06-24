// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareWithScopedTokensAllowsLocalIdentityAPITokenLifecycleRoutes(t *testing.T) {
	t.Parallel()

	routes := []string{
		"/api/v0/auth/local/api-tokens",
		"/api/v0/auth/local/api-tokens/token-old/revoke",
		"/api/v0/auth/local/api-tokens/token-old/rotate",
	}
	for _, path := range routes {
		t.Run(path, func(t *testing.T) {
			resolver := &fakeScopedTokenResolver{
				context: AuthContext{
					Mode:        AuthModeScoped,
					TenantID:    "tenant_a",
					WorkspaceID: "workspace_a",
					AllScopes:   true,
				},
				ok: true,
			}
			called := false
			handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				auth, ok := AuthContextFromContext(r.Context())
				if !ok {
					t.Fatal("AuthContextFromContext() ok = false, want true")
				}
				if auth.TenantID != "tenant_a" || auth.WorkspaceID != "workspace_a" || !auth.AllScopes {
					t.Fatalf("auth context = %#v, want all-scope tenant/workspace", auth)
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if !called {
				t.Fatal("next handler not called for local identity api token route")
			}
			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}
