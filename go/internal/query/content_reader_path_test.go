// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
)

func TestContentReaderListRepoFilesByPathScopesAndEscapes(t *testing.T) {
	t.Parallel()
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"},
		rows:    [][]driver.Value{{"repo-1", "100%_!/deep/a.go", "abc123", "", "h-1", int64(5), "go", ""}},
	}})

	files, err := NewContentReader(db).ListRepoFilesByPath(context.Background(), "repo-1", "100%_!/deep", 12)
	if err != nil {
		t.Fatalf("ListRepoFilesByPath() error = %v", err)
	}
	if len(files) != 1 || files[0].RelativePath != "100%_!/deep/a.go" {
		t.Fatalf("files = %+v", files)
	}
	query := recorder.queries[0]
	for _, want := range []string{"repo_id = $1", "relative_path LIKE $2 ESCAPE '!'", "ORDER BY relative_path", "LIMIT $3"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q: %s", want, query)
		}
	}
	if got, want := recorder.args[0][0], driver.Value("repo-1"); got != want {
		t.Fatalf("repo arg = %v, want %v", got, want)
	}
	if got, want := recorder.args[0][1], driver.Value("100!%!_!!/deep/%"); got != want {
		t.Fatalf("path pattern = %q, want %q", got, want)
	}
	if got := numericDriverValue(t, recorder.args[0][2]); got != 12 {
		t.Fatalf("limit arg = %d, want 12", got)
	}
}

func TestContentReaderListRepoFilesByPathRootUsesWholeRepo(t *testing.T) {
	t.Parallel()
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"},
		rows:    [][]driver.Value{{"repo-1", "a.go", "abc123", "", "h-1", int64(5), "go", ""}},
	}})
	if _, err := NewContentReader(db).ListRepoFilesByPath(context.Background(), "repo-1", "", 1); err != nil {
		t.Fatalf("ListRepoFilesByPath(root) error = %v", err)
	}
	if strings.Contains(recorder.queries[0], "LIKE") {
		t.Fatalf("root listing should retain whole-repo query: %s", recorder.queries[0])
	}
}
