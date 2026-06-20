package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareWithScopedTokensAllowsFactSchemaVersionRoutes(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/fact-schema-versions",
		"/api/v0/fact-schema-versions/repository",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{
				context: AuthContext{
					Mode:        AuthModeScoped,
					TenantID:    "tenant_a",
					WorkspaceID: "workspace_a",
				},
				ok: true,
			}
			handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				auth, ok := AuthContextFromContext(r.Context())
				if !ok {
					t.Fatal("AuthContextFromContext() ok = false, want true")
				}
				if auth.Mode != AuthModeScoped {
					t.Fatalf("auth.Mode = %q, want scoped", auth.Mode)
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

func TestAuthMiddlewareWithScopedTokensRejectsAdjacentFactSchemaVersionRoutes(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/fact-schema-versions/repository/nested",
		"/api/v0/fact-schema-versions/",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{
				context: AuthContext{
					Mode:        AuthModeScoped,
					TenantID:    "tenant_a",
					WorkspaceID: "workspace_a",
				},
				ok: true,
			}
			handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				t.Fatal("next handler called for adjacent fact-schema-version route")
			}))

			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusForbidden; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}
