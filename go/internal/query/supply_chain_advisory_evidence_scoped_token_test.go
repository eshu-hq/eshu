// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failingAdvisoryEvidenceStore struct {
	called bool
}

func (s *failingAdvisoryEvidenceStore) ListAdvisoryEvidence(
	context.Context,
	AdvisoryEvidenceFilter,
) ([]AdvisoryEvidenceRow, error) {
	s.called = true
	return nil, errors.New("broad advisory evidence read")
}

func TestAuthMiddlewareWithScopedTokensAllowsAdvisoryEvidenceRoute(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/advisories/evidence?cve_id=CVE-2026-0001&limit=10", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestAdvisoryEvidenceScopedTokenAllowsPublicAdvisoryByID(t *testing.T) {
	t.Parallel()

	// "Allow public advisory data": a scoped token with no repository grants
	// may still read global advisory evidence by id; the store IS consulted.
	store := &recordingAdvisoryEvidenceStore{
		rows: []AdvisoryEvidenceRow{{AdvisoryKey: "CVE-2026-0001", CanonicalID: "CVE-2026-0001"}},
	}
	handler := &SupplyChainHandler{Content: repositorySelectorReadModelContentStore(), AdvisoryEvidence: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/advisories/evidence?cve_id=CVE-2026-0001&limit=10", nil)
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
	if store.calls != 1 {
		t.Fatalf("advisory store calls = %d, want 1 (public advisory data is allowed)", store.calls)
	}
	if got, want := store.lastFilter.CVEID, "CVE-2026-0001"; got != want {
		t.Fatalf("CVEID = %q, want %q", got, want)
	}
}

func TestAdvisoryEvidenceScopedTokenDeniesOutOfGrantRepositoryBeforeStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingAdvisoryEvidenceStore{}
	handler := &SupplyChainHandler{Content: repositorySelectorReadModelContentStore(), AdvisoryEvidence: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/advisories/evidence?repository_id=payments-api&limit=10", nil)
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
		t.Fatal("advisory store was called for out-of-grant repository selector")
	}
	if strings.Contains(rec.Body.String(), "repo://example/api") {
		t.Fatalf("out-of-grant response leaked repository id: %s", rec.Body.String())
	}
}

func TestAdvisoryEvidenceScopedTokenPropagatesGrantsForRepositoryAnchor(t *testing.T) {
	t.Parallel()

	store := &recordingAdvisoryEvidenceStore{
		rows: []AdvisoryEvidenceRow{{AdvisoryKey: "CVE-2026-0001", CanonicalID: "CVE-2026-0001"}},
	}
	handler := &SupplyChainHandler{Content: repositorySelectorReadModelContentStore(), AdvisoryEvidence: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/advisories/evidence?repository_id=payments-api&limit=10", nil)
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
	wantGrants := []string{"git-repository-scope:example/api", "repo://example/api"}
	if got := store.lastFilter.AllowedSourceRepositoryIDs; !equalPacketStringSlices(got, wantGrants) {
		t.Fatalf("AllowedSourceRepositoryIDs = %#v, want %#v", got, wantGrants)
	}
}

func TestAdvisoryEvidenceSQLBoundsImpactSelectorByGrants(t *testing.T) {
	t.Parallel()

	const predicate = "fact.payload->>'repository_id' = ANY($9::text[])"
	if !strings.Contains(listAdvisoryEvidenceQuery, predicate) {
		t.Fatalf("advisory evidence query missing impact-selector grant predicate %q:\n%s", predicate, listAdvisoryEvidenceQuery)
	}
	// The grant predicate must apply inside the impact_candidates CTE (which
	// derives advisory anchors from impact findings), before the advisory fact
	// seeding in seed_candidates.
	if strings.Index(listAdvisoryEvidenceQuery, predicate) > strings.Index(listAdvisoryEvidenceQuery, "seed_candidates AS MATERIALIZED") {
		t.Fatalf("grant predicate must bound impact_candidates before seed_candidates:\n%s", listAdvisoryEvidenceQuery)
	}
}
