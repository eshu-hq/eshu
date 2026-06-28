// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareWithScopedTokensRejectsContractImpactRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant_a",
			WorkspaceID:          "workspace_a",
			AllowedRepositoryIDs: []string{"repo_a"},
		},
		ok: true,
	}
	called := false
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/contracts", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler called for contract-impact route under scoped token")
	}
	if got, want := rec.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}
