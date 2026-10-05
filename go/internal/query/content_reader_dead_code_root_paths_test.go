// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
)

// TestCrossRepoDeadCodeConsumerRootPathsOneKeyedRead pins the statement the
// test-only-consumers flag runs (#7603): the shipped constant, no source_cache,
// both arrays bound as parameters, and a path per returned root.
func TestCrossRepoDeadCodeConsumerRootPathsOneKeyedRead(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{"entity_id", "relative_path"},
		rows: [][]driver.Value{
			{"root-a", "pkg/charge_test.go"},
			{"root-b", "pkg/charge.go"},
		},
	}})
	got, err := NewContentReader(db).CrossRepoDeadCodeConsumerRootPaths(
		context.Background(),
		[]string{"root-b", "root-a", "root-a", " "},
		[]string{"repo-2", "repo-1"},
	)
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerRootPaths() error = %v", err)
	}
	if got["root-a"] != "pkg/charge_test.go" || got["root-b"] != "pkg/charge.go" || len(got) != 2 {
		t.Fatalf("paths = %#v, want root-a and root-b", got)
	}
	if len(recorder.queries) != 1 {
		t.Fatalf("queries = %d, want exactly 1 batched read", len(recorder.queries))
	}
	query := recorder.queries[0]
	if query != deadcode.CrossRepoDeadCodeConsumerRootPathsQuery {
		t.Fatalf("query = %q, want the shipped constant", query)
	}
	for _, want := range []string{"entity_id = ANY($1)", "repo_id = ANY($2)", "FROM content_entities"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q: %s", want, query)
		}
	}
	if strings.Contains(query, "source_cache") {
		t.Fatalf("query reads source_cache: %s", query)
	}
	if fmt.Sprint(recorder.args[0][0]) != `{"root-b","root-a"}` || fmt.Sprint(recorder.args[0][1]) != `{"repo-2","repo-1"}` {
		t.Fatalf("args = %v, want the cleaned root ids then the repository ids", recorder.args[0])
	}
}

// TestCrossRepoDeadCodeConsumerRootPathsSkipsEmptyInput keeps the read off the
// wire when there is nothing to look up.
func TestCrossRepoDeadCodeConsumerRootPathsSkipsEmptyInput(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, nil)
	reader := NewContentReader(db)
	for name, args := range map[string][2][]string{
		"no roots":        {nil, {"repo-1"}},
		"blank roots":     {{" ", ""}, {"repo-1"}},
		"no repositories": {{"root-a"}, nil},
	} {
		got, err := reader.CrossRepoDeadCodeConsumerRootPaths(context.Background(), args[0], args[1])
		if err != nil || len(got) != 0 {
			t.Fatalf("%s: got %#v, %v, want an empty map and no error", name, got, err)
		}
	}
	if len(recorder.queries) != 0 {
		t.Fatalf("queries = %d, want none", len(recorder.queries))
	}
}

// TestCrossRepoDeadCodeConsumerRootPathsCapsTheBatch keeps a caller from sending
// more ids than the evidence page can name.
func TestCrossRepoDeadCodeConsumerRootPathsCapsTheBatch(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{"entity_id", "relative_path"},
	}})
	ids := make([]string, 0, maxCrossRepoDeadCodeConsumerEvidenceRows+5)
	for i := 0; i < maxCrossRepoDeadCodeConsumerEvidenceRows+5; i++ {
		ids = append(ids, fmt.Sprintf("root-%04d", i))
	}
	if _, err := NewContentReader(db).CrossRepoDeadCodeConsumerRootPaths(context.Background(), ids, []string{"repo-1"}); err != nil {
		t.Fatalf("error = %v", err)
	}
	if got := strings.Count(fmt.Sprint(recorder.args[0][0]), `"root-`); got != maxCrossRepoDeadCodeConsumerEvidenceRows {
		t.Fatalf("ids sent = %d, want %d", got, maxCrossRepoDeadCodeConsumerEvidenceRows)
	}
}
