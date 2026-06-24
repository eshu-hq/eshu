// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
				Neo4j: fakeRepoGraphReader{
					run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
						runCyphers = append(runCyphers, cypher)
						return nil, nil
					},
					runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
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
						return repositoryStatsGraphRow(), nil
					},
				},
				Content: fakePortContentStore{
					coverage: RepositoryContentCoverage{
						Available:       true,
						FileCount:       42,
						EntityCount:     7,
						FileIndexedAt:   indexedAt.Add(-time.Minute),
						EntityIndexedAt: indexedAt,
						Languages: []RepositoryLanguageCount{
							{Language: "go", FileCount: 30},
							{Language: "yaml", FileCount: 12},
						},
						EntityTypes: []RepositoryEntityTypeCount{
							{EntityType: "Function", Count: 5},
							{EntityType: "TerraformResource", Count: 2},
						},
					},
					repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
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

			resp := decodeRepositoryStatsResponse(t, w)
			if got, want := resp["file_count"], float64(42); got != want {
				t.Fatalf("file_count = %#v, want %#v", got, want)
			}
			if got, want := resp["entity_count"], float64(7); got != want {
				t.Fatalf("entity_count = %#v, want %#v", got, want)
			}
			repositoryStatsRequireStringSlice(t, resp, "languages", []string{"go", "yaml"})
			repositoryStatsRequireStringSlice(t, resp, "entity_types", []string{"Function", "TerraformResource"})

			coverage := repositoryStatsRequireMap(t, resp, "coverage")
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
			repositoryStatsRequireStringSlice(t, coverage, "missing_evidence", nil)
		})
	}
}

func TestGetRepositoryStatsReportsMissingContentCoverageWithoutInventedTotals(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if strings.Contains(cypher, "OPTIONAL MATCH") || strings.Contains(cypher, "CONTAINS]->(e)") {
					t.Fatalf("missing-coverage stats path used broad graph aggregation:\n%s", cypher)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("repo_id param = %#v, want %#v", got, want)
				}
				return repositoryStatsGraphRow(), nil
			},
		},
		Content: fakePortContentStore{
			coverage: RepositoryContentCoverage{
				Available: true,
			},
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
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

	resp := decodeRepositoryStatsResponse(t, w)
	if got := resp["file_count"]; got != nil {
		t.Fatalf("file_count = %#v, want nil when content coverage is unavailable", got)
	}
	if got := resp["entity_count"]; got != nil {
		t.Fatalf("entity_count = %#v, want nil when content coverage is unavailable", got)
	}
	repositoryStatsRequireStringSlice(t, resp, "languages", nil)
	repositoryStatsRequireStringSlice(t, resp, "entity_types", nil)

	coverage := repositoryStatsRequireMap(t, resp, "coverage")
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
	repositoryStatsRequireStringSlice(t, coverage, "missing_evidence", []string{"content_store_coverage"})
}

func TestGetRepositoryStatsLogsMissingCoverageTelemetry(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": repositoryStatsGraphRow(),
			},
		},
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
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

func TestContentReaderRepositoryCoverageIncludesEntityTypeCounts(t *testing.T) {
	t.Parallel()

	fileIndexedAt := time.Date(2026, 5, 29, 11, 58, 0, 0, time.UTC)
	entityIndexedAt := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{columns: []string{"count"}, rows: [][]driver.Value{{int64(42)}}},
		{columns: []string{"count"}, rows: [][]driver.Value{{int64(7)}}},
		{columns: []string{"indexed_at"}, rows: [][]driver.Value{{fileIndexedAt}}},
		{columns: []string{"indexed_at"}, rows: [][]driver.Value{{entityIndexedAt}}},
		{
			columns: []string{"language", "file_count"},
			rows: [][]driver.Value{
				{"go", int64(30)},
				{"yaml", int64(12)},
			},
		},
		{
			columns: []string{"entity_type", "entity_count"},
			rows: [][]driver.Value{
				{"Function", int64(5)},
				{"TerraformResource", int64(2)},
			},
			queryContains: []string{"FROM content_entities", "GROUP BY entity_type", "ORDER BY entity_count DESC, entity_type"},
		},
	})

	coverage, err := NewContentReader(db).RepositoryCoverage(t.Context(), "repo-1")
	if err != nil {
		t.Fatalf("RepositoryCoverage() error = %v, want nil", err)
	}
	if got, want := coverage.EntityTypes, []RepositoryEntityTypeCount{
		{EntityType: "Function", Count: 5},
		{EntityType: "TerraformResource", Count: 2},
	}; !repositoryEntityTypeCountsEqual(got, want) {
		t.Fatalf("EntityTypes = %#v, want %#v", got, want)
	}
}

func repositoryStatsCatalogEntry() RepositoryCatalogEntry {
	return RepositoryCatalogEntry{
		ID:        "repo-1",
		Name:      "order-service",
		Path:      "/repos/order-service",
		LocalPath: "/repos/order-service",
		RemoteURL: "https://github.com/org/order-service",
		RepoSlug:  "org/order-service",
		HasRemote: true,
	}
}

func repositoryStatsGraphRow() map[string]any {
	return map[string]any{
		"id":         "repo-1",
		"name":       "order-service",
		"path":       "/repos/order-service",
		"local_path": "/repos/order-service",
		"remote_url": "https://github.com/org/order-service",
		"repo_slug":  "org/order-service",
		"has_remote": true,
	}
}

func decodeRepositoryStatsResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return resp
}

func repositoryStatsRequireMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()

	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%s type = %T, want map[string]any", key, parent[key])
	}
	return value
}

func repositoryStatsRequireStringSlice(t *testing.T, parent map[string]any, key string, want []string) {
	t.Helper()

	values, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("%s type = %T, want []any", key, parent[key])
	}
	got := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("%s item type = %T, want string", key, value)
		}
		got = append(got, text)
	}
	if len(got) != len(want) {
		t.Fatalf("%s = %#v, want %#v", key, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %#v, want %#v", key, got, want)
		}
	}
}

func repositoryEntityTypeCountsEqual(got, want []RepositoryEntityTypeCount) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
