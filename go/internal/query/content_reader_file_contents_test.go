// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

var getFileContentsColumns = []string{
	"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type",
}

// TestContentReaderGetFileContentsIssuesOneStatement pins the statement shape:
// one query, the repository and the distinct paths as bind values in that
// order, the GetFileContent column list, and `= ANY` equality rather than a
// pattern. An empty input issues none.
func TestContentReaderGetFileContentsIssuesOneStatement(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: getFileContentsColumns,
		rows: [][]driver.Value{
			{"repo-1", "b.yaml", "abc", "b: 1\n", "h-b", int64(1), "yaml", "config"},
			{"repo-1", "a b.tf", "", "x", "h-a", int64(1), "", ""},
		},
		queryContainsInOrder: []string{
			"SELECT repo_id, relative_path, coalesce(commit_sha, '')",
			"content, content_hash, line_count",
			"FROM content_files",
			"WHERE repo_id = $1 AND relative_path = ANY($2::text[])",
		},
		wantArgs: []driver.Value{"repo-1", `{"b.yaml","a b.tf"}`},
	}})
	reader := NewContentReader(db)

	files, err := reader.GetFileContents(context.Background(), "repo-1", []string{"b.yaml", "a b.tf", "b.yaml"})
	if err != nil {
		t.Fatalf("GetFileContents() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("rows = %d, want 2", len(files))
	}
	byPath := map[string]FileContent{}
	for _, file := range files {
		byPath[file.RelativePath] = file
	}
	if got := byPath["b.yaml"]; got.Content != "b: 1\n" || got.CommitSHA != "abc" || got.Language != "yaml" || got.ArtifactType != "config" || got.LineCount != 1 {
		t.Fatalf("b.yaml row = %+v", got)
	}
	if got := byPath["a b.tf"]; got.Content != "x" || got.RepoID != "repo-1" {
		t.Fatalf("a b.tf row = %+v", got)
	}

	// No queued answer is left for an empty input: it must not reach the driver.
	empty, err := reader.GetFileContents(context.Background(), "repo-1", nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty input = %v, %v, want no rows and no error", empty, err)
	}
}

// TestContentReaderGetFileContentsReturnsNoRowsOnFailure proves an error from
// the query or the scan never comes back as a partial result.
func TestContentReaderGetFileContentsReturnsNoRowsOnFailure(t *testing.T) {
	t.Parallel()

	t.Run("query error", func(t *testing.T) {
		t.Parallel()
		db := openContentReaderTestDB(t, []contentReaderQueryResult{{
			queryContains: []string{"relative_path = ANY"},
			err:           context.DeadlineExceeded,
		}})
		files, err := NewContentReader(db).GetFileContents(context.Background(), "repo-1", []string{"a"})
		if err == nil || !strings.Contains(err.Error(), "get file contents") || files != nil {
			t.Fatalf("files=%v err=%v, want no rows and a wrapped error", files, err)
		}
	})
	t.Run("scan error", func(t *testing.T) {
		t.Parallel()
		db := openContentReaderTestDB(t, []contentReaderQueryResult{{
			columns: getFileContentsColumns,
			rows: [][]driver.Value{
				{"repo-1", "a", "", "ok", "h", int64(1), "", ""},
				{"repo-1", "b", "", "bad", "h", "not-a-number", "", ""},
			},
			queryContains: []string{"relative_path = ANY"},
		}})
		files, err := NewContentReader(db).GetFileContents(context.Background(), "repo-1", []string{"a", "b"})
		if err == nil || files != nil {
			t.Fatalf("files=%v err=%v, want no rows and a scan error", files, err)
		}
	})
}

// rowsErrStore answers every query with one valid row and then a row-iteration
// error, the failure database/sql reports through Rows.Err after Next
// returns false.
type rowsErrStore struct {
	db.ReadStore
	err error
}

func (s rowsErrStore) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return &rowsErrRows{err: s.err}, nil
}

type rowsErrRows struct {
	served bool
	err    error
}

func (r *rowsErrRows) Next() bool {
	if r.served {
		return false
	}
	r.served = true
	return true
}

func (r *rowsErrRows) Scan(dest ...any) error {
	values := []any{"repo-1", "a", "", "ok", "h", 1, "", ""}
	for i, target := range dest {
		switch ptr := target.(type) {
		case *string:
			*ptr = values[i].(string)
		case *int:
			*ptr = values[i].(int)
		}
	}
	return nil
}

func (r *rowsErrRows) Err() error   { return r.err }
func (r *rowsErrRows) Close() error { return nil }

// TestContentReaderGetFileContentsRowsErrorReturnsNoRows proves a failure that
// surfaces only after the last row (a dropped connection mid-result) fails the
// call and returns no rows, so a truncated result is never read as complete.
func TestContentReaderGetFileContentsRowsErrorReturnsNoRows(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection reset mid result")
	reader := NewContentReaderWithReadStore(rowsErrStore{err: boom})
	files, err := reader.GetFileContents(context.Background(), "repo-1", []string{"a", "b"})
	if !errors.Is(err, boom) || files != nil {
		t.Fatalf("files=%v err=%v, want no rows and the iteration error", files, err)
	}
}
