// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetRepositoryStatsReturnsPartialMetadataWhenContentCoverageTimesOut(t *testing.T) {
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
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	resp := decodeRepositoryStatsResponse(t, w)
	if got := resp["file_count"]; got != nil {
		t.Fatalf("file_count = %#v, want nil for timed-out coverage", got)
	}
	if got := resp["entity_count"]; got != nil {
		t.Fatalf("entity_count = %#v, want nil for timed-out coverage", got)
	}

	coverage := repositoryStatsRequireMap(t, resp, "coverage")
	if got, want := coverage["partial_results"], true; got != want {
		t.Fatalf("coverage.partial_results = %#v, want %#v", got, want)
	}
	if got, want := coverage["truncated"], true; got != want {
		t.Fatalf("coverage.truncated = %#v, want %#v", got, want)
	}
	if got, want := coverage["timeout"], true; got != want {
		t.Fatalf("coverage.timeout = %#v, want %#v", got, want)
	}
	if got, want := coverage["timeout_budget"], "2s"; got != want {
		t.Fatalf("coverage.timeout_budget = %#v, want %#v", got, want)
	}
	repositoryStatsRequireStringSlice(t, coverage, "missing_evidence", []string{"content_store_coverage_timeout"})
	if got, want := coverage["last_error"], "content store coverage exceeded 2s route timeout"; got != want {
		t.Fatalf("coverage.last_error = %#v, want %#v", got, want)
	}
}

func TestGetRepositoryStatsSelectorResolutionUsesRouteDeadline(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			run: func(ctx context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				if err := requireRepositoryStatsDeadline(ctx); err != nil {
					return nil, err
				}
				return nil, context.DeadlineExceeded
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/order-service/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusGatewayTimeout; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got := w.Body.String(); !strings.Contains(got, "query graph repository selector") {
		t.Fatalf("body = %s, want selector query error detail", got)
	}
}

func TestGetRepositoryStatsReturnsLargeContentCoverageInsideBoundedShape(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	var runCyphers []string
	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				runCyphers = append(runCyphers, cypher)
				return nil, nil
			},
			runSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": repositoryStatsGraphRow(),
			},
		},
		Content: repositoryStatsDeadlineContentStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			},
			coverage: RepositoryContentCoverage{
				Available:       true,
				FileCount:       5_000_000,
				EntityCount:     4_200_000,
				FileIndexedAt:   indexedAt.Add(-time.Minute),
				EntityIndexedAt: indexedAt,
				Languages: []RepositoryLanguageCount{
					{Language: "go", FileCount: 3_000_000},
					{Language: "typescript", FileCount: 2_000_000},
				},
				EntityTypes: []RepositoryEntityTypeCount{
					{EntityType: "Function", Count: 3_500_000},
					{EntityType: "Type", Count: 700_000},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if len(runCyphers) != 0 {
		t.Fatalf("Run calls = %d, want 0; first query:\n%s", len(runCyphers), runCyphers[0])
	}

	resp := decodeRepositoryStatsResponse(t, w)
	if got, want := resp["file_count"], float64(5_000_000); got != want {
		t.Fatalf("file_count = %#v, want %#v", got, want)
	}
	if got, want := resp["entity_count"], float64(4_200_000); got != want {
		t.Fatalf("entity_count = %#v, want %#v", got, want)
	}

	coverage := repositoryStatsRequireMap(t, resp, "coverage")
	if got, want := coverage["query_shape"], repositoryStatsContentCoverageShape; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
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

type repositoryStatsDeadlineContentStore struct {
	fakePortContentStore
	coverage RepositoryContentCoverage
	err      error
}

func (s repositoryStatsDeadlineContentStore) RepositoryCoverage(
	ctx context.Context,
	repoID string,
) (RepositoryContentCoverage, error) {
	if repoID != "repo-1" {
		return RepositoryContentCoverage{}, fmt.Errorf("repoID = %q, want repo-1", repoID)
	}
	if err := requireRepositoryStatsDeadline(ctx); err != nil {
		return RepositoryContentCoverage{}, err
	}
	if s.err != nil {
		return RepositoryContentCoverage{}, s.err
	}
	return s.coverage, nil
}

func requireRepositoryStatsDeadline(ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("repository stats read called without route deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > 2*time.Second+250*time.Millisecond {
		return fmt.Errorf("repository stats deadline remaining = %s, want at most 2s", remaining)
	}
	return nil
}
