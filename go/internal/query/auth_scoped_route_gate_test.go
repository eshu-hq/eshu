// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

func TestAuthMiddlewareWithScopedTokensRejectsUnsupportedScopedRoute(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "/api/v0/code/search", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.Header.Set("Authorization", "Bearer scoped-token")
	req.Header.Set("X-Correlation-ID", "corr-scoped-route")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler called for unsupported scoped route")
	}
	if got, want := rec.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
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
	if got, want := envelope.Error.CorrelationID, "corr-scoped-route"; got != want {
		t.Fatalf("correlation_id = %q, want %q", got, want)
	}
}

func TestAuthMiddlewareWithScopedTokensRejectsScopedMutationOnAllowedPath(t *testing.T) {
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
	handler := AuthMiddlewareWithScopedTokens("", resolver, mockHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsSharedTokenOnUnsupportedRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{}
	handler := AuthMiddlewareWithScopedTokens("shared-token", resolver, mockHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v0/code/search", nil)
	req.Header.Set("Authorization", "Bearer shared-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsSurfaceInventoryRoute(t *testing.T) {
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
		if auth.AllScopes || len(auth.AllowedRepositoryIDs) != 0 || len(auth.AllowedScopeIDs) != 0 {
			t.Fatalf("auth context = %#v, want empty scoped grant", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v0/surface-inventory", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsRepositoryListWithEmptyGrant(t *testing.T) {
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
		if auth.AllScopes || len(auth.AllowedRepositoryIDs) != 0 || len(auth.AllowedScopeIDs) != 0 {
			t.Fatalf("auth context = %#v, want empty scoped grant", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsCodeSearchWithEmptyGrant(t *testing.T) {
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
		if auth.AllScopes || len(auth.AllowedRepositoryIDs) != 0 || len(auth.AllowedScopeIDs) != 0 {
			t.Fatalf("auth context = %#v, want empty scoped grant", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/search", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsEntityResolveWithEmptyGrant(t *testing.T) {
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
		if auth.AllScopes || len(auth.AllowedRepositoryIDs) != 0 || len(auth.AllowedScopeIDs) != 0 {
			t.Fatalf("auth context = %#v, want empty scoped grant", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsContentRoutesWithEmptyGrant(t *testing.T) {
	t.Parallel()

	routes := []struct {
		name   string
		method string
		path   string
	}{
		{name: "read file", method: http.MethodPost, path: "/api/v0/content/files/read"},
		{name: "read file lines", method: http.MethodPost, path: "/api/v0/content/files/lines"},
		{name: "read entity", method: http.MethodPost, path: "/api/v0/content/entities/read"},
		{name: "search files", method: http.MethodPost, path: "/api/v0/content/files/search"},
		{name: "search entities", method: http.MethodPost, path: "/api/v0/content/entities/search"},
	}
	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
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
				if auth.AllScopes || len(auth.AllowedRepositoryIDs) != 0 || len(auth.AllowedScopeIDs) != 0 {
					t.Fatalf("auth context = %#v, want empty scoped grant", auth)
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsEvidenceCitationRouteWithEmptyGrant(t *testing.T) {
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
		if auth.AllScopes || len(auth.AllowedRepositoryIDs) != 0 || len(auth.AllowedScopeIDs) != 0 {
			t.Fatalf("auth context = %#v, want empty scoped grant", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v0/evidence/citations", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsEntityContextRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:        AuthModeScoped,
			TenantID:    "tenant_a",
			WorkspaceID: "workspace_a",
			AllScopes:   true,
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, ok := AuthContextFromContext(r.Context())
		if !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		if !auth.AllScopes {
			t.Fatalf("auth.AllScopes = false, want true")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/service:payments/context", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestAuthMiddlewareWithScopedTokensRejectsAllScopeOnUnsupportedEntityRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:        AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
			AllScopes:   true,
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, mockHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/service:payments/relationships", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsServiceAndWorkloadContextRoutes(t *testing.T) {
	t.Parallel()

	routes := []string{
		"/api/v0/workloads/workload:payments/context",
		"/api/v0/workloads/workload:payments/story",
		"/api/v0/services/payments/context",
		"/api/v0/services/payments/story",
	}
	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{
				context: AuthContext{
					Mode:        AuthModeScoped,
					TenantID:    "tenant-a",
					WorkspaceID: "workspace-a",
					AllScopes:   true,
				},
				ok: true,
			}
			handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := AuthContextFromContext(r.Context()); !ok {
					t.Fatal("AuthContextFromContext() ok = false, want true")
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, route, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsServiceInvestigationRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:        AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
			AllScopes:   true,
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v0/investigations/services/payments", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestAuthMiddlewareWithScopedTokensAuditsUnsupportedScopedRoute(t *testing.T) {
	t.Parallel()

	audit := &fakeGovernanceAuditAppender{}
	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:               AuthModeScoped,
			TenantID:           "tenant_a",
			WorkspaceID:        "workspace_a",
			SubjectClass:       "team",
			SubjectIDHash:      "sha256:abcdef12",
			PolicyRevisionHash: "sha256:01234567",
			AllScopes:          true,
		},
		ok: true,
	}
	handler := authMiddleware("", resolver, nil, mockHandler(), audit, false)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/code/search", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	req.Header.Set("X-Correlation-ID", "corr-scoped-route")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := len(audit.events), 1; got != want {
		t.Fatalf("len(audit.events) = %d, want %d", got, want)
	}
	event := audit.events[0]
	if got, want := event.Type, governanceaudit.EventTypeReadAuthorization; got != want {
		t.Fatalf("event.Type = %q, want %q", got, want)
	}
	if got, want := event.ActorClass, governanceaudit.ActorClassScopedToken; got != want {
		t.Fatalf("event.ActorClass = %q, want %q", got, want)
	}
	if got, want := event.ActorIDHash, "sha256:abcdef12"; got != want {
		t.Fatalf("event.ActorIDHash = %q, want %q", got, want)
	}
	if got, want := event.PolicyRevisionHash, "sha256:01234567"; got != want {
		t.Fatalf("event.PolicyRevisionHash = %q, want %q", got, want)
	}
	if got, want := event.ReasonCode, "scoped_route_not_enabled"; got != want {
		t.Fatalf("event.ReasonCode = %q, want %q", got, want)
	}
	if _, err := governanceaudit.NormalizeEvent(event); err != nil {
		t.Fatalf("governanceaudit.NormalizeEvent() error = %v, want nil", err)
	}
}
