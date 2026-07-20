// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fatalOnCallPackageRegistryAggregateStore t.Fatal()s on any call, proving a
// scoped caller's explicit private/unknown visibility filter short-circuits
// to an empty envelope without ever reaching the aggregate store.
type fatalOnCallPackageRegistryAggregateStore struct {
	t *testing.T
}

func (s *fatalOnCallPackageRegistryAggregateStore) CountPackageRegistryPackages(
	context.Context,
	PackageRegistryAggregateFilter,
) (PackageRegistryAggregateCount, error) {
	s.t.Fatal("aggregate store was called for a scoped private/unknown visibility filter")
	return PackageRegistryAggregateCount{}, nil
}

func (s *fatalOnCallPackageRegistryAggregateStore) PackageRegistryPackageInventory(
	context.Context,
	PackageRegistryAggregateFilter,
	PackageRegistryInventoryDimension,
	int,
	int,
) ([]PackageRegistryInventoryRow, error) {
	s.t.Fatal("aggregate store was called for a scoped private/unknown visibility filter")
	return nil, nil
}

func TestPackageRegistryAggregateCountScopedForcesPublicVisibility(t *testing.T) {
	t.Parallel()
	store := &stubPackageRegistryAggregateStore{count: PackageRegistryAggregateCount{TotalPackages: 3, ByEcosystem: map[string]int{"npm": 3}}}
	handler := &PackageRegistryHandler{Aggregates: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/count", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), tenantAScopedAuthContext()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := store.lastFilter.Visibility, packageRegistryVisibilityPublic; got != want {
		t.Fatalf("store received Visibility = %q, want %q (forced)", got, want)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	scope, ok := body["scope"].(map[string]any)
	if !ok {
		t.Fatalf("scope missing or wrong shape: %#v", body["scope"])
	}
	if got, want := scope["visibility"], "public"; got != want {
		t.Fatalf("scope[visibility] = %v, want %q (honest reflection of the forced filter)", got, want)
	}
}

func TestPackageRegistryAggregateCountScopedPrivateFilterReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()
	store := &fatalOnCallPackageRegistryAggregateStore{t: t}
	handler := &PackageRegistryHandler{Aggregates: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/count?visibility=private", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), tenantAScopedAuthContext()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := rec.Body.String(), `"total_packages":0`; !strings.Contains(got, want) {
		t.Fatalf("body = %s, want to contain %q", got, want)
	}
}

func TestPackageRegistryAggregateInventoryScopedForcesPublicVisibilityAndDegeneratesGroupByVisibility(t *testing.T) {
	t.Parallel()
	store := &stubPackageRegistryAggregateStore{
		inventory: []PackageRegistryInventoryRow{{Dimension: PackageRegistryInventoryByVisibility, Value: "public", Count: 5}},
	}
	handler := &PackageRegistryHandler{Aggregates: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/inventory?group_by=visibility&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), tenantAScopedAuthContext()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := store.lastFilter.Visibility, packageRegistryVisibilityPublic; got != want {
		t.Fatalf("store received Visibility = %q, want %q (forced)", got, want)
	}
	if strings.Contains(rec.Body.String(), `"value":"private"`) || strings.Contains(rec.Body.String(), `"value":"unknown"`) {
		t.Fatalf("scoped group_by=visibility must degenerate to the public bucket only: %s", rec.Body.String())
	}
}

func TestPackageRegistryAggregateInventoryScopedPrivateFilterReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()
	store := &fatalOnCallPackageRegistryAggregateStore{t: t}
	handler := &PackageRegistryHandler{Aggregates: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/inventory?visibility=unknown&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), tenantAScopedAuthContext()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := rec.Body.String(), `"count":0`; !strings.Contains(got, want) {
		t.Fatalf("body = %s, want to contain %q", got, want)
	}
}

// TestPackageRegistryAggregateSharedKeyCallerUnaffected proves shared-key
// (unscoped) caller behavior is byte-identical to before the #5167 W5b
// change: no visibility forcing, and a private/unknown filter reaches the
// store exactly as requested.
func TestPackageRegistryAggregateSharedKeyCallerUnaffected(t *testing.T) {
	t.Parallel()
	store := &stubPackageRegistryAggregateStore{count: PackageRegistryAggregateCount{TotalPackages: 7, ByEcosystem: map[string]int{"npm": 7}}}
	handler := &PackageRegistryHandler{Aggregates: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/count?visibility=private", nil)
	// No AuthContext set: repositoryAccessFilterFromContext treats this as
	// allScopes (the same as AuthModeShared).
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := store.lastFilter.Visibility, "private"; got != want {
		t.Fatalf("store received Visibility = %q, want %q (unforced for shared-key caller)", got, want)
	}
	if got, want := store.countCalls, 1; got != want {
		t.Fatalf("store CountPackageRegistryPackages calls = %d, want %d", got, want)
	}
}
