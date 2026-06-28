// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
)

// TestContentReaderListRepoFilesByLanguagePushesPredicate proves the language
// filter is applied in SQL (so the LIMIT caps the matching set, not the whole
// repository) with the canonical column/ordering shape.
func TestContentReaderListRepoFilesByLanguagePushesPredicate(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{"repo-1", "zzz/late.py", "abc123", "", "h-1", int64(5), "python", ""},
			},
		},
	})

	reader := NewContentReader(db)
	results, err := reader.ListRepoFilesByLanguage(context.Background(), "repo-1", []string{"python"}, "src", 10)
	if err != nil {
		t.Fatalf("ListRepoFilesByLanguage() error = %v", err)
	}
	if len(results) != 1 || results[0].Language != "python" {
		t.Fatalf("results = %+v, want one python file", results)
	}
	query := recorder.queries[0]
	for _, want := range []string{
		// Bare normalized column (uses content_files_language_repo_idx), matching
		// the by-language inventory reads — not a function-wrapped column.
		"language = ANY($2::text[])",
		// Path scope pushed in before the LIMIT.
		"strpos(relative_path, $3 || '/') = 1",
		"ORDER BY relative_path",
		"LIMIT $4",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "lower(") {
		t.Fatalf("language match must use the bare indexed column, not lower():\n%s", query)
	}
}

// TestContentReaderListRepoFilesByLanguageEmptyFallsBack proves an empty language
// set delegates to the unfiltered listing (no language predicate), so callers can
// rely on a single method.
func TestContentReaderListRepoFilesByLanguageEmptyFallsBack(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{"repo-1", "a.go", "abc123", "", "h-1", int64(5), "go", ""},
			},
		},
	})

	reader := NewContentReader(db)
	if _, err := reader.ListRepoFilesByLanguage(context.Background(), "repo-1", nil, "", 10); err != nil {
		t.Fatalf("ListRepoFilesByLanguage(nil) error = %v", err)
	}
	if strings.Contains(recorder.queries[0], "ANY($2::text[])") {
		t.Fatalf("empty language set must not push a language predicate:\n%s", recorder.queries[0])
	}
}

// TestContentReaderRepoFilePathContextReportsExistenceAndRef proves the path/ref
// lookup resolves existence (file or directory prefix) and the indexed commit in
// a single unfiltered query, so the tree handler can distinguish an empty
// language listing from a missing path.
func TestContentReaderRepoFilePathContextReportsExistenceAndRef(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"path_exists", "indexed_ref"},
			rows:    [][]driver.Value{{true, "abc123"}},
		},
	})

	reader := NewContentReader(db)
	exists, ref, err := reader.RepoFilePathContext(context.Background(), "repo-1", "cmd/app")
	if err != nil {
		t.Fatalf("RepoFilePathContext() error = %v", err)
	}
	if !exists || ref != "abc123" {
		t.Fatalf("exists=%v ref=%q, want true/abc123", exists, ref)
	}
	query := recorder.queries[0]
	for _, want := range []string{"EXISTS", "strpos(relative_path, $2", "commit_sha"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}
