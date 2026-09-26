// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// listRepoFilesCountingStore counts ListRepoFiles calls made through it and
// records their arguments, delegating everything to the wrapped ContentStore.
type listRepoFilesCountingStore struct {
	querycontract.ContentStore
	calls atomic.Int32
	limit atomic.Int32
}

func (s *listRepoFilesCountingStore) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	s.calls.Add(1)
	s.limit.Store(int32(limit))
	return s.ContentStore.ListRepoFiles(ctx, repoID, limit)
}

// TestGetRepositoryStoryListsRepositoryFilesOnce pins #7126: the semantic
// overview and the content_files stage used to issue the identical
// ListRepoFiles(repoID, RepositorySemanticEntityLimit) read back to back, so a
// large repository paid for the same file list twice. The story must issue it
// exactly once and feed the one result to both consumers.
func TestGetRepositoryStoryListsRepositoryFilesOnce(t *testing.T) {
	t.Parallel()

	fileColumns := []string{
		"repo_id", "relative_path", "commit_sha", "content",
		"content_hash", "line_count", "language", "artifact_type",
	}
	compose := []driver.Value{"repo-once", "docker-compose.yaml", "abc123", "", "hash-compose", int64(20), "yaml", "docker_compose"}
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{columns: []string{
			"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
			"start_line", "end_line", "language", "source_cache", "metadata",
		}, rows: [][]driver.Value{}},
		{columns: fileColumns, rows: [][]driver.Value{compose}},
		// The narrative-file hydration reads the file content once more; the
		// story must not consume a second list result before it.
		{columns: fileColumns, rows: [][]driver.Value{compose}},
	})
	store := &listRepoFilesCountingStore{ContentStore: NewContentReader(db)}
	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"INSTANCE_OF": {
					"id": "repo-once", "name": "once", "path": "/repos/once", "local_path": "/repos/once",
					"remote_url": "https://github.com/acme/once", "repo_slug": "acme/once", "has_remote": true,
					"file_count": int64(1), "workload_count": int64(0), "platform_count": int64(0),
					"dependency_count": int64(0), "languages": []string{"yaml"},
					"workload_names": []string{}, "platform_types": []string{},
				},
			},
		},
		Content: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-once/story", nil)
	req.SetPathValue("repo_id", "repo-once")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if got := store.calls.Load(); got != 1 {
		t.Fatalf("ListRepoFiles calls = %d, want 1 (semantic overview and content_files share one read)", got)
	}
	// The cap plus one sentinel row, so a list past the cap is disclosed as
	// truncated instead of being clipped silently (#7126).
	if got, want := int(store.limit.Load()), querycontract.RepositorySemanticEntityLimit+1; got != want {
		t.Fatalf("ListRepoFiles limit = %d, want %d", got, want)
	}
}
