// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetRepositoryStatsExposesResultLimitsAndPartialReasons proves the
// singleton stats route carries the additive result_limits drilldown block and
// an explicit (empty) partial_reasons slot for a fully available read, while the
// existing coverage partial_results/truncated/timeout fields are preserved.
func TestGetRepositoryStatsExposesResultLimitsAndPartialReasons(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": repositoryStatsGraphRow(),
			},
		},
		Content: fakePortContentStore{
			coverage: RepositoryContentCoverage{
				Available:   true,
				FileCount:   42,
				EntityCount: 7,
				Languages: []RepositoryLanguageCount{
					{Language: "go", FileCount: 30},
					{Language: "yaml", FileCount: 12},
				},
				EntityTypes: []RepositoryEntityTypeCount{
					{EntityType: "Function", Count: 5},
				},
			},
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
		},
		Profile: ProfileProduction,
	}

	resp := serveRepositoryStats(t, handler, "/api/v0/repositories/repo-1/stats")

	limits := repositoryStatsRequireMap(t, resp, "result_limits")
	if got, want := limits["limit"], float64(repositoryStatsItemLimit); got != want {
		t.Fatalf("result_limits.limit = %#v, want %#v", got, want)
	}
	if got, want := StringVal(limits, "ordering"), "deterministic"; got != want {
		t.Fatalf("result_limits.ordering = %q, want %q", got, want)
	}
	if got, want := limits["language_count"], float64(2); got != want {
		t.Fatalf("result_limits.language_count = %#v, want %#v", got, want)
	}
	if got, want := limits["entity_type_count"], float64(1); got != want {
		t.Fatalf("result_limits.entity_type_count = %#v, want %#v", got, want)
	}
	if got, ok := limits["truncated"].(bool); !ok || got {
		t.Fatalf("result_limits.truncated = %#v, want false", limits["truncated"])
	}
	if got, want := StringVal(limits, "drilldown_tool"), "get_repository_coverage"; got != want {
		t.Fatalf("result_limits.drilldown_tool = %q, want %q", got, want)
	}
	if got, want := StringVal(limits, "context_path"), "/api/v0/repositories/repo-1/stats"; got != want {
		t.Fatalf("result_limits.context_path = %q, want %q", got, want)
	}

	reasons, ok := resp["partial_reasons"].([]any)
	if !ok {
		t.Fatalf("partial_reasons type = %T, want []any", resp["partial_reasons"])
	}
	if len(reasons) != 0 {
		t.Fatalf("partial_reasons = %#v, want empty for available coverage", reasons)
	}

	// Coverage fields remain intact (additive guarantee).
	coverage := repositoryStatsRequireMap(t, resp, "coverage")
	if got, want := coverage["partial_results"], false; got != want {
		t.Fatalf("coverage.partial_results = %#v, want %#v", got, want)
	}
	if got, want := coverage["truncated"], false; got != want {
		t.Fatalf("coverage.truncated = %#v, want %#v", got, want)
	}
	if got, want := coverage["timeout"], false; got != want {
		t.Fatalf("coverage.timeout = %#v, want %#v", got, want)
	}
}

// TestGetRepositoryStatsPartialReasonsPopulatedOnTimeout proves a timed-out
// coverage read surfaces explicit partial_reasons while preserving the existing
// coverage transport flags.
func TestGetRepositoryStatsPartialReasonsPopulatedOnTimeout(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": repositoryStatsGraphRow(),
			},
		},
		Content: repositoryStatsDeadlineContentStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			},
			err: context.DeadlineExceeded,
		},
		Profile: ProfileProduction,
	}

	resp := serveRepositoryStats(t, handler, "/api/v0/repositories/repo-1/stats")

	reasons := requireStringAnySlice(t, resp, "partial_reasons")
	if !anySliceContains(reasons, "content_store_coverage_timeout") {
		t.Fatalf("partial_reasons = %#v, want content_store_coverage_timeout", reasons)
	}

	coverage := repositoryStatsRequireMap(t, resp, "coverage")
	if got, want := coverage["timeout"], true; got != want {
		t.Fatalf("coverage.timeout = %#v, want %#v", got, want)
	}
	if got, want := coverage["partial_results"], true; got != want {
		t.Fatalf("coverage.partial_results = %#v, want %#v", got, want)
	}
}

// TestListRepositoriesInventoryExposesResultLimitsAndPartialReasons proves the
// inventory (empty-selector) form of get_repository_stats carries the additive
// result_limits drilldown block and explicit partial_reasons slot, including a
// truncated paging case.
func TestListRepositoriesInventoryExposesResultLimitsAndPartialReasons(t *testing.T) {
	t.Parallel()

	entries := make([]RepositoryCatalogEntry, 0, 3)
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		entries = append(entries, RepositoryCatalogEntry{ID: name + "-id", Name: name})
	}
	handler := &RepositoryHandler{
		Content: fakePortContentStore{repositories: entries},
		Profile: ProfileProduction,
	}

	resp := serveRepositoryStats(t, handler, "/api/v0/repositories?limit=2")

	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	limits := repositoryStatsRequireMap(t, resp, "result_limits")
	if got, want := limits["limit"], float64(2); got != want {
		t.Fatalf("result_limits.limit = %#v, want %#v", got, want)
	}
	if got, want := StringVal(limits, "ordering"), "deterministic"; got != want {
		t.Fatalf("result_limits.ordering = %q, want %q", got, want)
	}
	if got, want := StringVal(limits, "drilldown_tool"), "get_repository_stats"; got != want {
		t.Fatalf("result_limits.drilldown_tool = %q, want %q", got, want)
	}
	if got, want := StringVal(limits, "context_path"), "/api/v0/repositories"; got != want {
		t.Fatalf("result_limits.context_path = %q, want %q", got, want)
	}
	if got, ok := limits["truncated"].(bool); !ok || !got {
		t.Fatalf("result_limits.truncated = %#v, want true", limits["truncated"])
	}

	reasons := requireStringAnySlice(t, resp, "partial_reasons")
	if !anySliceContains(reasons, "repository_inventory_truncated") {
		t.Fatalf("partial_reasons = %#v, want repository_inventory_truncated", reasons)
	}
}

func serveRepositoryStats(t *testing.T, handler *RepositoryHandler, target string) map[string]any {
	t.Helper()

	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	return decodeRepositoryStatsResponse(t, w)
}

func requireStringAnySlice(t *testing.T, parent map[string]any, key string) []any {
	t.Helper()

	value, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("%s type = %T, want []any", key, parent[key])
	}
	return value
}

func anySliceContains(values []any, want string) bool {
	for _, value := range values {
		if s, ok := value.(string); ok && s == want {
			return true
		}
	}
	return false
}
