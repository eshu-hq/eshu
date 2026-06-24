// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPackageRegistryCorrelationsResolveRepositorySelectors(t *testing.T) {
	t.Parallel()

	store := &recordingPackageRegistryCorrelationStore{
		rows: []PackageRegistryCorrelationRow{{
			CorrelationID:    "correlation-1",
			RelationshipKind: "consumption",
			PackageID:        "pkg:npm/example",
			RepositoryID:     "repo://example/api",
			Outcome:          "exact",
		}},
	}
	handler := &PackageRegistryHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/correlations?repository_id=payments-api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
}

func TestPackageRegistryCorrelationsRejectUnknownRepositorySelector(t *testing.T) {
	t.Parallel()

	store := &recordingPackageRegistryCorrelationStore{}
	handler := &PackageRegistryHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/correlations?repository_id=unknown-repo&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastFilter.RepositoryID != "" {
		t.Fatalf("store received filter %#v, want no store call", store.lastFilter)
	}
}

func TestCICDRunCorrelationsResolveRepositorySelectors(t *testing.T) {
	t.Parallel()

	store := &recordingCICDRunCorrelationStore{
		rows: []CICDRunCorrelationRow{{
			CorrelationID: "correlation-1",
			RepositoryID:  "repo://example/api",
			Outcome:       "exact",
		}},
	}
	handler := &CICDHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations?repository_id=payments-api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
}

func TestCICDRunCorrelationAggregatesResolveRepositorySelectors(t *testing.T) {
	t.Parallel()

	store := &stubCICDRunCorrelationAggregateStore{}
	handler := &CICDHandler{
		Content:    repositorySelectorReadModelContentStore(),
		Aggregates: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations/count?repository_id=payments-api",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
}

func TestServiceCatalogCorrelationsResolveRepositorySelectors(t *testing.T) {
	t.Parallel()

	store := &recordingServiceCatalogCorrelationStore{
		rows: []ServiceCatalogCorrelationRow{{
			CorrelationID: "correlation-1",
			RepositoryID:  "repo://example/api",
			Outcome:       "exact",
		}},
	}
	handler := &ServiceCatalogHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/service-catalog/correlations?repository_id=payments-api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
}

func TestSupplyChainImpactExplainResolvesRepositorySelectors(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactExplanationStore{
		row: exactManifestAndImageExplanationRow(),
	}
	handler := &SupplyChainHandler{
		Content:            repositorySelectorReadModelContentStore(),
		ImpactExplanations: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?cve_id=CVE-2026-0001&package_id=pkg:npm/example&repository_id=payments-api",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
}

func repositorySelectorReadModelContentStore() fakePortContentStore {
	return fakePortContentStore{
		repositories: []RepositoryCatalogEntry{{
			ID:        "repo://example/api",
			Name:      "payments-api",
			LocalPath: "/srv/payments-api",
			RepoSlug:  "example/payments-api",
		}},
	}
}
