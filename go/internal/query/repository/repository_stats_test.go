// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestGetRepositoryStatsUsesContentCoverageForRepositoryNameAndCanonicalID(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		path string
	}{
		{name: "repository name", path: "/api/v0/repositories/order-service/stats"},
		{name: "canonical id", path: "/api/v0/repositories/repo-1/stats"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var runCyphers []string
			var runSingleCyphers []string
			handler := &RepositoryHandler{
				Neo4j: querytestutil.FakeRepoGraphReader{
					RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
						runCyphers = append(runCyphers, cypher)
						return nil, nil
					},
					RunSingleFn: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
						runSingleCyphers = append(runSingleCyphers, cypher)
						if strings.Contains(cypher, "OPTIONAL MATCH") || strings.Contains(cypher, "CONTAINS]->(e)") {
							t.Fatalf("stats query used broad graph aggregation:\n%s", cypher)
						}
						if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
							t.Fatalf("stats query = %s, want canonical repository id lookup", cypher)
						}
						if got, want := params["repo_id"], "repo-1"; got != want {
							t.Fatalf("repo_id param = %#v, want %#v", got, want)
						}
						return querytestutil.RepositoryStatsGraphRow(), nil
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

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if len(runCyphers) != 0 {
				t.Fatalf("Run calls = %d, want 0; first query:\n%s", len(runCyphers), runCyphers[0])
			}
			if len(runSingleCyphers) != 1 {
				t.Fatalf("RunSingle calls = %d, want 1", len(runSingleCyphers))
			}

			resp := querytestutil.DecodeResponseBody(t, w)
			if got, want := resp["file_count"], float64(42); got != want {
				t.Fatalf("file_count = %#v, want %#v", got, want)
			}
			if got, want := resp["entity_count"], float64(7); got != want {
				t.Fatalf("entity_count = %#v, want %#v", got, want)
			}
			querytestutil.RequireStringSlice(t, resp, "languages", []string{"go", "yaml"})
			querytestutil.RequireStringSlice(t, resp, "entity_types", []string{"Function", "TerraformResource"})

			coverage := querytestutil.MustMapField(t, resp, "coverage")
			if got, want := coverage["source_backend"], "content_store"; got != want {
				t.Fatalf("coverage.source_backend = %#v, want %#v", got, want)
			}
			if got, want := coverage["query_shape"], "content_store_repository_coverage"; got != want {
				t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
			}
			if got, want := coverage["counts_available"], true; got != want {
				t.Fatalf("coverage.counts_available = %#v, want %#v", got, want)
			}
			if got, want := coverage["whole_graph_traversal"], false; got != want {
				t.Fatalf("coverage.whole_graph_traversal = %#v, want %#v", got, want)
			}
			querytestutil.RequireStringSlice(t, coverage, "missing_evidence", nil)
		})
	}
}

func TestGetRepositoryStatsReportsMissingContentCoverageWithoutInventedTotals(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: querytestutil.FakeRepoGraphReader{
			RunSingleFn: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if strings.Contains(cypher, "OPTIONAL MATCH") || strings.Contains(cypher, "CONTAINS]->(e)") {
					t.Fatalf("missing-coverage stats path used broad graph aggregation:\n%s", cypher)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("repo_id param = %#v, want %#v", got, want)
				}
				return querytestutil.RepositoryStatsGraphRow(), nil
			},
		},
		Content: querytestutil.FakePortContentStore{
			Coverage: querycontract.RepositoryContentCoverage{
				Available: true,
			},
			Repositories: []querycontract.RepositoryCatalogEntry{querytestutil.RepositoryStatsCatalogEntry()},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/order-service/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	resp := querytestutil.DecodeResponseBody(t, w)
	if got := resp["file_count"]; got != nil {
		t.Fatalf("file_count = %#v, want nil when content coverage is unavailable", got)
	}
	if got := resp["entity_count"]; got != nil {
		t.Fatalf("entity_count = %#v, want nil when content coverage is unavailable", got)
	}
	querytestutil.RequireStringSlice(t, resp, "languages", nil)
	querytestutil.RequireStringSlice(t, resp, "entity_types", nil)

	coverage := querytestutil.MustMapField(t, resp, "coverage")
	if got, want := coverage["source_backend"], "unavailable"; got != want {
		t.Fatalf("coverage.source_backend = %#v, want %#v", got, want)
	}
	if got, want := coverage["query_shape"], "repository_identity_only"; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
	if got, want := coverage["counts_available"], false; got != want {
		t.Fatalf("coverage.counts_available = %#v, want %#v", got, want)
	}
	if got, want := coverage["whole_graph_traversal"], false; got != want {
		t.Fatalf("coverage.whole_graph_traversal = %#v, want %#v", got, want)
	}
	querytestutil.RequireStringSlice(t, coverage, "missing_evidence", []string{"content_store_coverage"})
}

func TestGetRepositoryStatsLogsMissingCoverageTelemetry(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := &RepositoryHandler{
		Neo4j: querytestutil.FakeRepoGraphReader{
			RunSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": querytestutil.RepositoryStatsGraphRow(),
			},
		},
		Content: querytestutil.FakePortContentStore{
			Repositories: []querycontract.RepositoryCatalogEntry{querytestutil.RepositoryStatsCatalogEntry()},
		},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/order-service/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	logText := logs.String()
	for _, want := range []string{
		`"event_name":"repository_query.stage_started"`,
		`"event_name":"repository_query.stage_completed"`,
		`"operation":"repository_stats"`,
		`"stage":"repository_lookup"`,
		`"stage":"content_coverage"`,
		`"query_shape":"repository_identity_only"`,
		`"source_backend":"unavailable"`,
		`"counts_available":false`,
		`"duration_seconds"`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s; logs = %s", want, logText)
		}
	}
}
