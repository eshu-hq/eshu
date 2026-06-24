// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeServiceCatalogCorrelationStore is a test double for
// ServiceCatalogCorrelationStore that returns a fixed slice of rows and records
// the filter passed by the caller.
type fakeServiceCatalogCorrelationStore struct {
	rows   []ServiceCatalogCorrelationRow
	filter ServiceCatalogCorrelationFilter
}

func (f *fakeServiceCatalogCorrelationStore) ListServiceCatalogCorrelations(
	_ context.Context,
	filter ServiceCatalogCorrelationFilter,
) ([]ServiceCatalogCorrelationRow, error) {
	f.filter = filter
	return f.rows, nil
}

func TestListCatalogEnrichesWorkloadsFromCorrelations(t *testing.T) {
	t.Parallel()

	rows := catalogGraphRows{
		repositories: []map[string]any{},
		base: []map[string]any{
			{"id": "workload:svc-alpha", "name": "svc-alpha", "kind": "service"},
			{"id": "workload:svc-beta", "name": "svc-beta", "kind": "service"},
		},
		repo: []map[string]any{
			{"id": "workload:svc-alpha", "repo_id": "repository:r_alpha", "repo_name": "svc-alpha"},
			{"id": "workload:svc-beta", "repo_id": "repository:r_beta", "repo_name": "svc-beta"},
		},
		instance: []map[string]any{},
	}

	store := &fakeServiceCatalogCorrelationStore{
		rows: []ServiceCatalogCorrelationRow{
			{
				RepositoryID: "repository:r_alpha",
				Tier:         "tier-1",
				EntityType:   "service",
				OwnerRef:     "team-platform",
			},
		},
	}

	handler := &RepositoryHandler{
		Neo4j:                      rows.reader(t, 0),
		Profile:                    ProfileLocalAuthoritative,
		ServiceCatalogCorrelations: store,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=10", nil)
	rec := httptest.NewRecorder()
	handler.listCatalog(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	workloads, _ := body["workloads"].([]any)
	if len(workloads) != 2 {
		t.Fatalf("len(workloads) = %d, want 2; body = %s", len(workloads), rec.Body.String())
	}

	// svc-alpha matches the correlation; svc-beta does not.
	for _, raw := range workloads {
		w, _ := raw.(map[string]any)
		name, _ := w["name"].(string)
		switch name {
		case "svc-alpha":
			if got, want := w["tier"], "tier-1"; got != want {
				t.Errorf("svc-alpha tier = %q, want %q", got, want)
			}
			if got, want := w["category"], "service"; got != want {
				t.Errorf("svc-alpha category = %q, want %q", got, want)
			}
			if got, want := w["domain"], "team-platform"; got != want {
				t.Errorf("svc-alpha domain = %q, want %q", got, want)
			}
		case "svc-beta":
			// No matching correlation — enrichment fields must be absent or empty string.
			if tier, ok := w["tier"]; ok && tier != "" {
				t.Errorf("svc-beta tier = %q, want absent/empty", tier)
			}
		default:
			t.Errorf("unexpected workload name %q", name)
		}
	}

	// The store must have been called with AllowedRepositoryIDs covering both
	// repo IDs so the single Postgres query is bounded to the catalog set.
	wantIDs := map[string]struct{}{
		"repository:r_alpha": {},
		"repository:r_beta":  {},
	}
	for _, id := range store.filter.AllowedRepositoryIDs {
		delete(wantIDs, id)
	}
	if len(wantIDs) != 0 {
		t.Errorf("AllowedRepositoryIDs missing %v", wantIDs)
	}
}

func TestListCatalogSkipsCorrelationEnrichmentWhenStoreIsNil(t *testing.T) {
	t.Parallel()

	rows := catalogGraphRows{
		repositories: []map[string]any{},
		base: []map[string]any{
			{"id": "workload:svc-alpha", "name": "svc-alpha", "kind": "service"},
		},
		repo: []map[string]any{
			{"id": "workload:svc-alpha", "repo_id": "repository:r_alpha", "repo_name": "svc-alpha"},
		},
		instance: []map[string]any{},
	}

	// ServiceCatalogCorrelations intentionally left nil — must not panic.
	handler := &RepositoryHandler{Neo4j: rows.reader(t, 0), Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=10", nil)
	rec := httptest.NewRecorder()
	handler.listCatalog(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

func TestServiceCatalogCorrelationFilterHasScopeWithAllowedRepositoryIDs(t *testing.T) {
	t.Parallel()

	// hasScope() must return true when only AllowedRepositoryIDs is set so the
	// catalog enrichment path can issue a scope-bounded lookup without a required
	// single-id field (AllowedRepositoryIDs becomes the SQL $13 array predicate).
	f := ServiceCatalogCorrelationFilter{
		AllowedRepositoryIDs: []string{"repository:r_alpha"},
	}
	if !f.hasScope() {
		t.Fatal("hasScope() = false with non-empty AllowedRepositoryIDs, want true")
	}

	// Empty filter must still return false.
	empty := ServiceCatalogCorrelationFilter{}
	if empty.hasScope() {
		t.Fatal("hasScope() = true for empty filter, want false")
	}
}
