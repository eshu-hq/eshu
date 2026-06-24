// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareWithScopedTokensAllowsQueryPlaybookRoutes(t *testing.T) {
	t.Parallel()

	routes := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "list playbooks",
			method: http.MethodGet,
			path:   "/api/v0/query-playbooks",
		},
		{
			name:   "resolve playbook",
			method: http.MethodPost,
			path:   "/api/v0/query-playbooks/resolve",
		},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{
				context: AuthContext{
					Mode:        AuthModeScoped,
					TenantID:    "tenant-a",
					WorkspaceID: "workspace-a",
				},
				ok: true,
			}
			handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := AuthContextFromContext(r.Context()); !ok {
					t.Fatal("AuthContextFromContext() ok = false, want true")
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(route.method, route.path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}
