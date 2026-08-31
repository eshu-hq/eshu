// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// These two tests moved back from internal/query/packagereg with the rest of
// the package-registry handler family's tests (#6060), because they exercise
// AuthMiddlewareWithScopedTokens's route allowlist directly against a stub
// terminal handler -- neither calls PackageRegistryHandler or Mount, so they
// are a root auth-middleware concern (proving these package-registry paths
// clear the allowlist) rather than family behavior, and
// AuthMiddlewareWithScopedTokens has no querycontract/queryauth-style leaf a
// family package could import without an import cycle back through root.

func TestAuthMiddlewareWithScopedTokensAllowsPackageRegistryCorrelationRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/correlations?repository_id=repo-team-a&limit=10",
		nil,
	)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

// TestAuthMiddlewareWithScopedTokensAllowsPackageRegistryIdentityRoutes
// proves the 5 package-registry identity/aggregate routes (#5167 W5b) clear
// AuthMiddlewareWithScopedTokens for a scoped bearer token and reach the
// inner handler -- these routes were previously blocked with a middleware
// 403 (see the removed
// TestAuthMiddlewareWithScopedTokensRejectsPackageRegistryAdjacentRoutes)
// because they sat in pendingRowFilteringRoutes; each handler now applies
// its own visibility/correlation-grant gate
// (packagereg/package_registry_scoped_access.go) on top of this middleware
// allowlist entry.
func TestAuthMiddlewareWithScopedTokensAllowsPackageRegistryIdentityRoutes(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, target := range []string{
		"/api/v0/package-registry/packages?ecosystem=npm&limit=10",
		"/api/v0/package-registry/versions?package_id=pkg:npm://registry.example/team-api&limit=10",
		"/api/v0/package-registry/dependencies?package_id=pkg:npm://registry.example/team-api&limit=10",
		"/api/v0/package-registry/packages/count",
		"/api/v0/package-registry/packages/inventory?limit=10",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusNoContent; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}
