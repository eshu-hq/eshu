// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestGetRepositoryStoryUsesContentCoverageWhenStatsAndCoverageRoutesHaveCounts(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 6, 6, 15, 30, 0, 0, time.UTC)
	handler := &Handler{
		Neo4j: querytestutil.FakeRepoGraphReader{
			RunSingleFn: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if strings.Contains(cypher, "count(DISTINCT e) as entity_count") {
					t.Fatalf("repository coverage stats graph fallback ran despite content coverage:\n%s", cypher)
				}
				if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
					t.Fatalf("RunSingle cypher = %q, want repository base lookup", cypher)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("repo_id param = %#v, want %#v", got, want)
				}
				return querytestutil.RepositoryStatsGraphRow(), nil
			},
			RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "count(DISTINCT e) as entity_count") {
					t.Fatalf("repository coverage stats graph fallback ran despite content coverage:\n%s", cypher)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("repo_id param = %#v, want %#v", got, want)
				}
				switch {
				case strings.Contains(cypher, "RETURN w.name AS workload_name"):
					return []map[string]any{{"workload_name": "order-service"}}, nil
				case strings.Contains(cypher, "RETURN p.type AS platform_type"):
					return []map[string]any{{"platform_type": "ecs"}}, nil
				case strings.Contains(cypher, "RETURN count(DISTINCT dep) AS count"):
					return []map[string]any{{"count": int64(1)}}, nil
				default:
					return nil, nil
				}
			},
		},
		Content: querytestutil.FakePortContentStore{
			Coverage: querycontract.RepositoryContentCoverage{
				Available:       true,
				FileCount:       42,
				EntityCount:     7,
				FileIndexedAt:   indexedAt.Add(-time.Minute),
				EntityIndexedAt: indexedAt,
				Languages: []querycontract.RepositoryLanguageCount{
					{Language: "go", FileCount: 30},
					{Language: "yaml", FileCount: 12},
				},
				EntityTypes: []querycontract.RepositoryEntityTypeCount{
					{EntityType: "Function", Count: 5},
					{EntityType: "TerraformResource", Count: 2},
				},
			},
			Repositories: []querycontract.RepositoryCatalogEntry{querytestutil.RepositoryStatsCatalogEntry()},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	story := serveRepositoryJSON(t, mux, "/api/v0/repositories/order-service/story")
	stats := serveRepositoryJSON(t, mux, "/api/v0/repositories/order-service/stats")
	coverage := serveRepositoryJSON(t, mux, "/api/v0/repositories/order-service/coverage")

	coverageSummary := querytestutil.MustMapField(t, story, "coverage_summary")
	if got, want := coverageSummary["status"], "available"; got != want {
		t.Fatalf("coverage_summary.status = %#v, want %#v", got, want)
	}
	if got, want := coverageSummary["source_backend"], "content_store"; got != want {
		t.Fatalf("coverage_summary.source_backend = %#v, want %#v", got, want)
	}
	if got, want := coverageSummary["query_shape"], repositoryStatsContentCoverageShape; got != want {
		t.Fatalf("coverage_summary.query_shape = %#v, want %#v", got, want)
	}
	if got, want := coverageSummary["whole_graph_traversal"], false; got != want {
		t.Fatalf("coverage_summary.whole_graph_traversal = %#v, want %#v", got, want)
	}
	if got, want := coverageSummary["file_count"], stats["file_count"]; got != want {
		t.Fatalf("coverage_summary.file_count = %#v, want stats.file_count %#v", got, want)
	}
	if got, want := coverageSummary["entity_count"], stats["entity_count"]; got != want {
		t.Fatalf("coverage_summary.entity_count = %#v, want stats.entity_count %#v", got, want)
	}
	if got, want := coverageSummary["file_count"], coverage["file_count"]; got != want {
		t.Fatalf("coverage_summary.file_count = %#v, want coverage.file_count %#v", got, want)
	}
	if got, want := coverageSummary["entity_count"], coverage["entity_count"]; got != want {
		t.Fatalf("coverage_summary.entity_count = %#v, want coverage.entity_count %#v", got, want)
	}
	querytestutil.RequireStringSlice(t, coverageSummary, "missing_evidence", nil)
	assertRepositoryStoryLacksLimitation(t, story, "coverage_not_computed")
}

func TestGetRepositoryStoryReportsMissingContentCoverageReason(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Neo4j: querytestutil.FakeRepoGraphReader{
			RunSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": querytestutil.RepositoryStatsGraphRow(),
			},
			RunFn: querytestutil.StoryEnvelopeGraphRows(t, "repo-1"),
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	story := serveRepositoryJSON(t, mux, "/api/v0/repositories/repo-1/story")
	coverageSummary := querytestutil.MustMapField(t, story, "coverage_summary")
	if got, want := coverageSummary["status"], "unavailable"; got != want {
		t.Fatalf("coverage_summary.status = %#v, want %#v", got, want)
	}
	if got, want := coverageSummary["query_shape"], repositoryStatsIdentityOnlyShape; got != want {
		t.Fatalf("coverage_summary.query_shape = %#v, want %#v", got, want)
	}
	querytestutil.RequireStringSlice(t, coverageSummary, "missing_evidence", []string{"content_store_coverage"})
	assertRepositoryStoryHasLimitation(t, story, "content_store_coverage")
	assertRepositoryStoryLacksLimitation(t, story, "coverage_not_computed")
}

func serveRepositoryJSON(t *testing.T, mux *http.ServeMux, path string) map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("%s status = %d, want %d; body = %s", path, got, want, w.Body.String())
	}
	return querytestutil.DecodeResponseBody(t, w)
}

func assertRepositoryStoryHasLimitation(t *testing.T, story map[string]any, want string) {
	t.Helper()

	limitations, ok := story["limitations"].([]any)
	if !ok {
		t.Fatalf("limitations type = %T, want []any", story["limitations"])
	}
	for _, limitation := range limitations {
		if limitation == want {
			return
		}
	}
	t.Fatalf("limitations = %#v, want %q", limitations, want)
}

func assertRepositoryStoryLacksLimitation(t *testing.T, story map[string]any, forbidden string) {
	t.Helper()

	limitations, ok := story["limitations"].([]any)
	if !ok {
		t.Fatalf("limitations type = %T, want []any", story["limitations"])
	}
	for _, limitation := range limitations {
		if limitation == forbidden {
			t.Fatalf("limitations = %#v, want no %q", limitations, forbidden)
		}
	}
}
