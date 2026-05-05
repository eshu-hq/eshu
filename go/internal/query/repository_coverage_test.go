package query

import (
	"database/sql/driver"
	"testing"
	"time"
)

func TestQueryContentStoreCoverageIncludesCompletenessAndGapFields(t *testing.T) {
	t.Parallel()

	contentIndexedAt := time.Date(2026, 4, 18, 15, 4, 5, 0, time.UTC)
	entityIndexedAt := time.Date(2026, 4, 18, 15, 9, 5, 0, time.UTC)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"count"},
			rows:    [][]driver.Value{{int64(10)}},
		},
		{
			columns: []string{"count"},
			rows:    [][]driver.Value{{int64(7)}},
		},
		{
			columns: []string{"indexed_at"},
			rows:    [][]driver.Value{{contentIndexedAt}},
		},
		{
			columns: []string{"indexed_at"},
			rows:    [][]driver.Value{{entityIndexedAt}},
		},
		{
			columns: []string{"language", "file_count"},
			rows: [][]driver.Value{
				{"go", int64(8)},
				{"yaml", int64(2)},
			},
		},
	})

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"count(DISTINCT e) as entity_count": {
					"file_count":   int64(12),
					"entity_count": int64(9),
				},
			},
		},
		Content: NewContentReader(db),
	}

	got, err := handler.queryContentStoreCoverage(t.Context(), "repo-coverage")
	if err != nil {
		t.Fatalf("queryContentStoreCoverage() error = %v, want nil", err)
	}

	if got, want := got["repo_id"], "repo-coverage"; got != want {
		t.Fatalf("repo_id = %#v, want %#v", got, want)
	}
	if got, want := got["file_count"], 10; got != want {
		t.Fatalf("file_count = %#v, want %#v", got, want)
	}
	if got, want := got["entity_count"], 7; got != want {
		t.Fatalf("entity_count = %#v, want %#v", got, want)
	}
	if got, want := got["graph_available"], true; got != want {
		t.Fatalf("graph_available = %#v, want %#v", got, want)
	}
	if got, want := got["server_content_available"], true; got != want {
		t.Fatalf("server_content_available = %#v, want %#v", got, want)
	}
	if got, want := got["content_gap_count"], 4; got != want {
		t.Fatalf("content_gap_count = %#v, want %#v", got, want)
	}
	if got, want := got["graph_gap_count"], 0; got != want {
		t.Fatalf("graph_gap_count = %#v, want %#v", got, want)
	}
	if got, want := got["completeness_state"], "content_partial"; got != want {
		t.Fatalf("completeness_state = %#v, want %#v", got, want)
	}
	if got, want := got["content_last_indexed_at"], entityIndexedAt.Format(time.RFC3339Nano); got != want {
		t.Fatalf("content_last_indexed_at = %#v, want %#v", got, want)
	}
	if got, want := got["last_error"], ""; got != want {
		t.Fatalf("last_error = %#v, want %#v", got, want)
	}

	summary, ok := got["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary type = %T, want map[string]any", got["summary"])
	}
	if got, want := summary["graph_file_count"], 12; got != want {
		t.Fatalf("summary.graph_file_count = %#v, want %#v", got, want)
	}
	if got, want := summary["graph_entity_count"], 9; got != want {
		t.Fatalf("summary.graph_entity_count = %#v, want %#v", got, want)
	}
	if got, want := summary["content_file_count"], 10; got != want {
		t.Fatalf("summary.content_file_count = %#v, want %#v", got, want)
	}
	if got, want := summary["content_entity_count"], 7; got != want {
		t.Fatalf("summary.content_entity_count = %#v, want %#v", got, want)
	}
}
