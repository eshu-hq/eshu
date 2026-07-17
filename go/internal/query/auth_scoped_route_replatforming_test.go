// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareWithScopedTokensAllowsReplatformingSelectorsWithEmptyGrant(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:        AuthModeScoped,
			TenantID:    "tenant_a",
			WorkspaceID: "workspace_a",
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, mockHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v0/replatforming/selectors", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, recorder.Body.String())
	}
}
