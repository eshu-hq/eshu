// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// serviceReadModelInfrastructureOverflowContentStore is a ContentStore double
// for TestGetServiceContextReadModelFallbackDisclosesInfrastructureTruncated
// below. It reaches fetchServiceReadModelWorkloadContext specifically (rather
// than the graph-backed fetchWorkloadContextForOperation path exercised by the
// workload-id and repository-story regressions), which requires satisfying
// two additional seams that function reads through:
//
//   - ResolveRepository, via the embedded fakePortContentStore's repositories
//     field, so the repository-identity lookup resolves.
//   - RepositoryReadModelSummary, the unexported repositoryReadModelSummaryStore
//     interface loadRepositoryReadModelSummary type-asserts for; without it,
//     that loader returns nil immediately and fetchServiceReadModelWorkloadContext
//     never reaches its infrastructure read at all.
//
// ListRepoEntitiesByTypes is overridden the same way as
// infrastructureOverflowContentStore (context_story_limits_test.go): it
// ignores the type list and limit arguments and returns every seeded row
// unconditionally, so production's own
// len(entities) > repositoryInfrastructureEntityLimit check -- not a
// client-side fake clamp -- decides truncation.
type serviceReadModelInfrastructureOverflowContentStore struct {
	fakePortContentStore
	infrastructureEntities []EntityContent
	workloadNames          []string
}

func (s serviceReadModelInfrastructureOverflowContentStore) ListRepoEntitiesByTypes(_ context.Context, _ string, _ []string, _ int) ([]EntityContent, error) {
	return s.infrastructureEntities, nil
}

func (s serviceReadModelInfrastructureOverflowContentStore) RepositoryReadModelSummary(
	_ context.Context, _ string,
) (RepositoryReadModelSummary, error) {
	return RepositoryReadModelSummary{Available: true, WorkloadNames: s.workloadNames}, nil
}

var _ repositoryReadModelSummaryStore = serviceReadModelInfrastructureOverflowContentStore{}

// TestGetServiceContextReadModelFallbackDisclosesInfrastructureTruncated is
// the PR #5933 review follow-up (P1-2). fetchServiceReadModelWorkloadContext
// (entity_workload_context.go:159-212) is the read-model fallback
// fetchServiceWorkloadContext (called by GET /api/v0/services/{name}/context)
// takes when no graph Workload node is materialized for the service but a
// repository read-model summary names it as a workload. That fallback's own
// infrastructure_truncated wiring was fixed alongside
// fetchWorkloadContextForOperation's, but nothing in this package drove it
// with an overflowing infrastructure set -- the mutation "gut the
// infrastructureTruncated plumbing there" left every existing test green.
// This drives the real mounted route through that exact fallback path (no
// graph Workload lookup ever matches, forcing fetchServiceReadModelWorkloadContext)
// with a genuine repositoryInfrastructureEntityLimit+1-row overflow, and
// proves "infrastructure_truncated" lands in the wire response's limitations.
func TestGetServiceContextReadModelFallbackDisclosesInfrastructureTruncated(t *testing.T) {
	t.Parallel()

	content := serviceReadModelInfrastructureOverflowContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{ID: "repo-1", Name: "order-service"}},
		},
		infrastructureEntities: overflowingInfrastructureEntities(repositoryInfrastructureEntityLimit + 1),
		workloadNames:          []string{"order-service"},
	}
	// No runSingleByMatch/runByMatch entries at all: every graph lookup
	// fetchServiceWorkloadContext tries (w.name = $service_name, then
	// w.id = $service_name) returns nil, which is what forces the fallback to
	// fetchServiceReadModelWorkloadContext in the first place.
	handler := &EntityHandler{
		Neo4j:   fakeWorkloadGraphReader{},
		Content: content,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/order-service/context", nil)
	req.SetPathValue("service_name", "order-service")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error = %v", err)
	}
	if got, want := StringVal(body, "materialization_status"), "identity_only"; got != want {
		t.Fatalf(
			"materialization_status = %q, want %q (the request must actually reach the read-model fallback, not the graph-backed path)",
			got, want,
		)
	}
	limitations := StringSliceVal(body, "limitations")
	if !slices.Contains(limitations, "infrastructure_truncated") {
		t.Fatalf(
			"limitations = %v, want it to contain %q (a genuine %d-row infrastructure overflow through fetchServiceReadModelWorkloadContext)",
			limitations, "infrastructure_truncated", repositoryInfrastructureEntityLimit+1,
		)
	}
}
