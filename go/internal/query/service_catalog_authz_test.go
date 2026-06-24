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

func TestAuthMiddlewareWithScopedTokensAllowsServiceCatalogCorrelationRoute(t *testing.T) {
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
		"/api/v0/service-catalog/correlations?repository_id=repo-team-a&limit=10",
		nil,
	)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestServiceCatalogScopedEmptyGrantReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingServiceCatalogCorrelationStore{}
	handler := &ServiceCatalogHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: store,
		Profile:      ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/service-catalog/correlations?repository_id=payments-api&limit=10",
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
		t.Fatal("correlation or descriptor store was called for empty scoped grants")
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	if got, want := int(body["count"].(float64)), 0; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if _, ok := body["missing_evidence"]; ok {
		t.Fatalf("missing_evidence present for empty scoped grants: %#v", body["missing_evidence"])
	}
	for _, leaked := range []string{"repo://example/api", "payments-api", "component:default"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Fatalf("empty scoped response leaked %q: %s", leaked, rec.Body.String())
		}
	}
}

func TestServiceCatalogScopedRepositorySelectorDeniesOutOfGrantWithoutStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingServiceCatalogCorrelationStore{}
	handler := &ServiceCatalogHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: store,
		Profile:      ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/service-catalog/correlations?repository_id=payments-api&limit=10",
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
		t.Fatal("correlation or descriptor store was called for out-of-grant repository selector")
	}
	for _, leaked := range []string{"repo://example/api", "missing_evidence", "component:default"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Fatalf("out-of-grant response leaked %q: %s", leaked, rec.Body.String())
		}
	}
}

func TestServiceCatalogHandlerPassesScopedGrants(t *testing.T) {
	t.Parallel()

	store := &recordingServiceCatalogCorrelationStore{
		rows: []ServiceCatalogCorrelationRow{{
			CorrelationID: "catalog-correlation-1",
			RepositoryID:  "repo://example/api",
			Outcome:       "exact",
		}},
	}
	handler := &ServiceCatalogHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: store,
		Profile:      ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/service-catalog/correlations?repository_id=payments-api&owner_ref=group:default/team-a&limit=10",
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
	if got, want := store.lastFilter.OwnerRef, "group:default/team-a"; got != want {
		t.Fatalf("OwnerRef = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.AllowedRepositoryIDs, []string{"repo://example/api"}; !equalPacketStringSlices(got, want) {
		t.Fatalf("AllowedRepositoryIDs = %#v, want %#v", got, want)
	}
	if got, want := store.lastFilter.AllowedScopeIDs, []string{"git-repository-scope:example/api"}; !equalPacketStringSlices(got, want) {
		t.Fatalf("AllowedScopeIDs = %#v, want %#v", got, want)
	}
}

func TestServiceCatalogCorrelationSQLAppliesScopedAuthorizationBeforeOrder(t *testing.T) {
	t.Parallel()

	query := listServiceCatalogCorrelationsQuery
	for _, want := range []string{
		"fact.scope_id = ANY($14::text[])",
		"fact.payload->>'repository_id' = ANY($13::text[])",
		"fact.payload->'candidate_repository_ids' ?| $13::text[]",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
		if strings.Index(query, want) > strings.Index(query, "ORDER BY") {
			t.Fatalf("authorization predicate %q appears after ORDER BY:\n%s", want, query)
		}
	}
}

type failingServiceCatalogCorrelationStore struct {
	called bool
}

func (s *failingServiceCatalogCorrelationStore) ListServiceCatalogCorrelations(
	context.Context,
	ServiceCatalogCorrelationFilter,
) ([]ServiceCatalogCorrelationRow, error) {
	s.called = true
	return nil, errors.New("broad service catalog correlation read")
}

func (s *failingServiceCatalogCorrelationStore) ListServiceCatalogLocalDescriptorEvidence(
	context.Context,
	string,
	int,
) ([]ServiceCatalogLocalDescriptorEvidenceRow, error) {
	s.called = true
	return nil, errors.New("broad service catalog descriptor read")
}
