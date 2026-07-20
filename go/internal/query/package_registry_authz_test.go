// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

func TestPackageRegistryScopedEmptyGrantReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingPackageRegistryCorrelationStore{}
	handler := &PackageRegistryHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: store,
		Profile:      ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/correlations?package_id=pkg:npm://registry.example/team-api&limit=10",
		nil,
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.called {
		t.Fatal("correlation store was called for empty scoped grants")
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	if got, want := int(body["count"].(float64)), 0; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	for _, leaked := range []string{"pkg:npm://registry.example/team-api", "repo://example/api", "payments-api"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Fatalf("empty scoped response leaked %q: %s", leaked, rec.Body.String())
		}
	}
}

func TestPackageRegistryScopedRepositorySelectorDeniesOutOfGrantWithoutStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingPackageRegistryCorrelationStore{}
	handler := &PackageRegistryHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: store,
		Profile:      ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/correlations?repository_id=payments-api&limit=10",
		nil,
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo://example/other"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.called {
		t.Fatal("correlation store was called for out-of-grant repository selector")
	}
	for _, leaked := range []string{"repo://example/api", "pkg:npm://registry.example", "candidate_repository_ids"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Fatalf("out-of-grant response leaked %q: %s", leaked, rec.Body.String())
		}
	}
}

func TestPackageRegistryHandlerPassesScopedGrants(t *testing.T) {
	t.Parallel()

	store := &recordingPackageRegistryCorrelationStore{
		rows: []PackageRegistryCorrelationRow{{
			CorrelationID:    "package-correlation-1",
			RelationshipKind: "publication",
			RepositoryID:     "repo://example/api",
			Outcome:          "exact",
		}},
	}
	handler := &PackageRegistryHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: store,
		Profile:      ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/correlations?repository_id=payments-api&relationship_kind=publication&limit=10",
		nil,
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo://example/api"},
		AllowedScopeIDs:      []string{"git-repository-scope:example/api"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := store.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.RelationshipKind, "publication"; got != want {
		t.Fatalf("RelationshipKind = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.AllowedRepositoryIDs, []string{"repo://example/api"}; !equalPacketStringSlices(got, want) {
		t.Fatalf("AllowedRepositoryIDs = %#v, want %#v", got, want)
	}
	if got, want := store.lastFilter.AllowedScopeIDs, []string{"git-repository-scope:example/api"}; !equalPacketStringSlices(got, want) {
		t.Fatalf("AllowedScopeIDs = %#v, want %#v", got, want)
	}
}

func TestPackageRegistryCorrelationSQLAppliesScopedAuthorizationBeforeOrder(t *testing.T) {
	t.Parallel()

	query := listPackageRegistryCorrelationsQuery
	for _, want := range []string{
		"fact.scope_id = ANY($8::text[])",
		"fact.payload->>'repository_id' = ANY($7::text[])",
		"fact.payload->'candidate_repository_ids' ?| $7::text[]",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
		if strings.Index(query, want) > strings.Index(query, "ORDER BY") {
			t.Fatalf("authorization predicate %q appears after ORDER BY:\n%s", want, query)
		}
	}
}

// TestAuthMiddlewareWithScopedTokensAllowsPackageRegistryIdentityRoutes
// proves the 5 package-registry identity/aggregate routes (#5167 W5b) clear
// AuthMiddlewareWithScopedTokens for a scoped bearer token and reach the
// inner handler -- these routes were previously blocked with a middleware
// 403 (see the removed
// TestAuthMiddlewareWithScopedTokensRejectsPackageRegistryAdjacentRoutes)
// because they sat in pendingRowFilteringRoutes; each handler now applies
// its own visibility/correlation-grant gate (package_registry_scoped_access.go)
// on top of this middleware allowlist entry.
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

type failingPackageRegistryCorrelationStore struct {
	called bool
}

func (s *failingPackageRegistryCorrelationStore) ListPackageRegistryCorrelations(
	context.Context,
	PackageRegistryCorrelationFilter,
) ([]PackageRegistryCorrelationRow, error) {
	s.called = true
	return nil, errors.New("broad package registry correlation read")
}
