// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- dependencies: two-tenant DATA test (both package_id and version_id anchors) + empty grant ---

func TestPackageRegistryDependenciesScopedVisibilityAndGrant(t *testing.T) {
	t.Parallel()

	depRow := map[string]any{
		"dependency_id": "dep:1", "source_package_id": "pkg:npm:private-lib", "source_version_id": "pv:1",
		"dependency_package_id": "pkg:npm:leftpad",
	}

	t.Run("granted tenant sees dependencies via package_id anchor", func(t *testing.T) {
		t.Parallel()
		graph := &packageRegistryScopedGraphFake{
			t:                     t,
			visibilityByPackageID: map[string]string{"pkg:npm:private-lib": "private"},
			rows:                  []map[string]any{depRow},
		}
		correlations := &recordingPackageRegistryCorrelationStore{
			rows: []PackageRegistryCorrelationRow{{CorrelationID: "corr-a", PackageID: "pkg:npm:private-lib", RepositoryID: "repo-a"}},
		}
		handler := &PackageRegistryHandler{Neo4j: graph, Correlations: correlations, Profile: ProfileProduction}
		mux := http.NewServeMux()
		handler.Mount(mux)

		req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/dependencies?package_id=pkg:npm:private-lib&limit=10", nil)
		req = req.WithContext(ContextWithAuthContext(req.Context(), tenantAScopedAuthContext()))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "dep:1") {
			t.Fatalf("expected the granted tenant's dependency in response: %s", rec.Body.String())
		}
	})

	t.Run("ungranted tenant sees the same empty page as nonexistent package_id", func(t *testing.T) {
		t.Parallel()
		graph := &packageRegistryScopedGraphFake{
			t:                     t,
			visibilityByPackageID: map[string]string{"pkg:npm:private-lib": "private"},
			rows:                  []map[string]any{depRow},
		}
		correlations := &recordingPackageRegistryCorrelationStore{rows: nil}
		handler := &PackageRegistryHandler{Neo4j: graph, Correlations: correlations, Profile: ProfileProduction}
		mux := http.NewServeMux()
		handler.Mount(mux)

		req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/dependencies?package_id=pkg:npm:private-lib&limit=10", nil)
		req = req.WithContext(ContextWithAuthContext(req.Context(), tenantBScopedAuthContext()))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "dep:1") {
			t.Fatalf("ungranted response leaked dependency identity: %s", rec.Body.String())
		}
	})

	t.Run("granted tenant sees dependencies via version_id anchor resolution", func(t *testing.T) {
		t.Parallel()
		graph := &packageRegistryScopedGraphFake{
			t:                     t,
			visibilityByPackageID: map[string]string{"pkg:npm:private-lib": "private"},
			packageIDByVersionID:  map[string]string{"pv:1": "pkg:npm:private-lib"},
			rows:                  []map[string]any{depRow},
		}
		correlations := &recordingPackageRegistryCorrelationStore{
			rows: []PackageRegistryCorrelationRow{{CorrelationID: "corr-a", PackageID: "pkg:npm:private-lib", RepositoryID: "repo-a"}},
		}
		handler := &PackageRegistryHandler{Neo4j: graph, Correlations: correlations, Profile: ProfileProduction}
		mux := http.NewServeMux()
		handler.Mount(mux)

		req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/dependencies?version_id=pv:1&limit=10", nil)
		req = req.WithContext(ContextWithAuthContext(req.Context(), tenantAScopedAuthContext()))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "dep:1") {
			t.Fatalf("expected the version_id-anchored dependency in response: %s", rec.Body.String())
		}
		if got, want := correlations.lastFilter.PackageID, "pkg:npm:private-lib"; got != want {
			t.Fatalf("correlation probe resolved PackageID = %q, want %q (must resolve via version_id anchor)", got, want)
		}
	})
}

func TestPackageRegistryDependenciesEmptyGrantReturnsEmptyWithoutAnyStoreRead(t *testing.T) {
	t.Parallel()
	graph := &packageRegistryScopedGraphFake{t: t, fatalOnCall: true}
	handler := &PackageRegistryHandler{Neo4j: graph, Correlations: &fatalOnCallPackageRegistryCorrelationStore{t: t}, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/dependencies?package_id=pkg:npm:anything&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}
