// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"
)

func TestContentReaderReadRepositoriesByLanguageCombinesAggregateAndPage(t *testing.T) {
	t.Parallel()
	indexedAt := time.Date(2026, 5, 23, 14, 0, 0, 0, time.UTC)
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{"repository_count", "total_file_count", "aggregate_indexed_at", "repo_id", "repo_name", "path", "local_path", "remote_url", "repo_slug", "has_remote", "language", "file_count", "page_indexed_at"},
		rows:    [][]driver.Value{{int64(2), int64(10), indexedAt, "repository:web", "web", "/src/web", "/src/web", "", "acme/web", false, "tsx", int64(3), indexedAt}, {int64(2), int64(10), indexedAt, "repository:web", "web", "/src/web", "/src/web", "", "acme/web", false, "typescript", int64(4), indexedAt}},
	}})
	aggregate, repos, err := NewContentReader(db).ReadRepositoriesByLanguage(context.Background(), []string{"typescript", "tsx"}, 2, 0, false, []string{"repository:web"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.RepositoryCount != 2 || aggregate.FileCount != 10 || !aggregate.LastIndexedAt.Equal(indexedAt) {
		t.Fatalf("aggregate = %#v", aggregate)
	}
	if len(repos) != 1 || repos[0].FileCount != 7 || len(repos[0].Languages) != 2 {
		t.Fatalf("repos = %#v", repos)
	}
	if len(recorder.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(recorder.queries))
	}
	query := recorder.queries[0]
	for _, want := range []string{"language_rows AS MATERIALIZED", "repo_totals AS MATERIALIZED", "WHERE language = ANY($1) AND (repo_id = ANY($4) OR repo_id = ANY($5))", "FROM aggregate LEFT JOIN page ON true", "ORDER BY page.total_file_count DESC, page.repo_name, page.repo_id, language_rows.language"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q: %s", want, query)
		}
	}
}

func TestContentReaderReadRepositoriesByLanguageKeepsAggregateWithoutPage(t *testing.T) {
	t.Parallel()
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{"repository_count", "total_file_count", "aggregate_indexed_at", "repo_id", "repo_name", "path", "local_path", "remote_url", "repo_slug", "has_remote", "language", "file_count", "page_indexed_at"},
		rows:    [][]driver.Value{{int64(9), int64(123), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil}},
	}})
	aggregate, repos, err := NewContentReader(db).ReadRepositoriesByLanguage(context.Background(), []string{"go"}, 10, 1000, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.RepositoryCount != 9 || aggregate.FileCount != 123 || len(repos) != 0 {
		t.Fatalf("aggregate = %#v; repos = %#v", aggregate, repos)
	}
	if len(recorder.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(recorder.queries))
	}
}

func TestContentReaderReadRepositoriesByLanguageKeepsZeroAggregate(t *testing.T) {
	t.Parallel()
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{"repository_count", "total_file_count", "aggregate_indexed_at", "repo_id", "repo_name", "path", "local_path", "remote_url", "repo_slug", "has_remote", "language", "file_count", "page_indexed_at"},
		rows:    [][]driver.Value{{int64(0), int64(0), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil}},
	}})
	aggregate, repos, err := NewContentReader(db).ReadRepositoriesByLanguage(context.Background(), []string{"unknown-language"}, 11, 0, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.RepositoryCount != 0 || aggregate.FileCount != 0 || !aggregate.LastIndexedAt.IsZero() || len(repos) != 0 {
		t.Fatalf("aggregate = %#v; repos = %#v", aggregate, repos)
	}
	if len(recorder.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(recorder.queries))
	}
}

func TestContentReaderReadRepositoriesByLanguageCountOnlyPreservesAggregate(t *testing.T) {
	t.Parallel()
	indexedAt := time.Date(2026, 5, 23, 14, 0, 0, 0, time.UTC)
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{"repository_count", "file_count", "last_indexed_at"},
		rows:    [][]driver.Value{{int64(2), int64(10), indexedAt}},
	}})
	aggregate, repos, err := NewContentReader(db).ReadRepositoriesByLanguage(context.Background(), []string{"go"}, 0, 0, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.RepositoryCount != 2 || aggregate.FileCount != 10 || !aggregate.LastIndexedAt.Equal(indexedAt) || len(repos) != 0 {
		t.Fatalf("aggregate = %#v; repos = %#v", aggregate, repos)
	}
	if len(recorder.queries) != 1 || !strings.Contains(recorder.queries[0], "COUNT(DISTINCT repo_id)") {
		t.Fatalf("queries = %#v, want one count-only query", recorder.queries)
	}
}
