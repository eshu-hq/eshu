// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type countedReadStore struct {
	db.ReadStore
	rows, singles, snapshots int
}

func (store *countedReadStore) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	store.rows++
	return store.ReadStore.QueryContext(ctx, query, args...)
}

func (store *countedReadStore) QueryRowContext(ctx context.Context, query string, args ...any) db.Row {
	store.singles++
	return store.ReadStore.QueryRowContext(ctx, query, args...)
}

func (store *countedReadStore) BeginReadOnlySnapshot(ctx context.Context) (db.ReadTransaction, error) {
	store.snapshots++
	return store.ReadStore.BeginReadOnlySnapshot(ctx)
}

func TestContentReaderUsesReadStoreForRowCursorAndSnapshot(t *testing.T) {
	t.Parallel()

	sqlDB := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns:       []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"},
			rows:          [][]driver.Value{{"repo-1", "a.go", "abc", "package a", "hash", int64(1), "go", "source"}},
			queryContains: []string{"FROM content_files", "relative_path = $2"},
		},
		{
			columns:       []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"},
			rows:          [][]driver.Value{},
			queryContains: []string{"FROM content_files", "content ILIKE"},
		},
		secretLinesReadinessResult(true),
		secretLinesReadinessResult(true),
		{
			columns:       []string{"repo_id", "relative_path", "language", "line_number", "line_text", "finding_kind"},
			rows:          [][]driver.Value{},
			queryContains: []string{"FROM content_file_secret_lines"},
		},
	})
	guard := &countedReadStore{ReadStore: postgres.NewSQLReadStore(sqlDB)}
	reader := NewContentReaderWithReadStore(guard)

	file, err := reader.GetFileContent(context.Background(), "repo-1", "a.go")
	if err != nil || file == nil || file.Content != "package a" {
		t.Fatalf("GetFileContent() = (%+v, %v)", file, err)
	}
	_, err = reader.SearchFileContent(context.Background(), "repo-1", "pattern", 1)
	if err != nil {
		t.Fatalf("SearchFileContent() error = %v", err)
	}
	_, source, err := reader.InvestigateHardcodedSecretsWithSource(context.Background(), codequery.HardcodedSecretInvestigationRequest{RepoID: "repo-1", Limit: 1})
	if err != nil || source != codequery.HardcodedSecretReadSideTable {
		t.Fatalf("InvestigateHardcodedSecretsWithSource() = (%q, %v)", source, err)
	}
	if guard.singles != 2 || guard.rows != 1 || guard.snapshots != 1 {
		t.Fatalf("read store calls = row %d, cursor %d, snapshot %d; want 2, 1, 1", guard.singles, guard.rows, guard.snapshots)
	}
}
