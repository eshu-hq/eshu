// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAuthMiddlewareWithScopedTokensAllowsSemanticSearchRoute proves the
// scoped-token middleware admits the semantic-search route and puts the
// resolved grant snapshot in the request context.
//
// This is a middleware test, not a semantic-search test: the route is only the
// vehicle. It stays in this package with the rest of the scoped-token sweep
// rather than moving with the handler family (#6060), because
// AuthMiddlewareWithScopedTokens and the fakeScopedTokenResolver double it
// drives both live here.
func TestAuthMiddlewareWithScopedTokensAllowsSemanticSearchRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-payments"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v0/search/semantic", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}
