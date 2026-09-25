// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// semanticListStore serves synthetic bounded entity and file lists to the
// repository story and records the limit each read asked for. Every other read
// falls through to the wrapped reader.
type semanticListStore struct {
	querycontract.ContentStore
	entityCount, fileCount         int
	entityLimit, fileLimit         atomic.Int32
	entityRequested, fileRequested atomic.Bool
}

func (s *semanticListStore) ListRepoEntities(_ context.Context, repoID string, limit int) ([]querycontract.EntityContent, error) {
	s.entityRequested.Store(true)
	s.entityLimit.Store(int32(limit))
	rows := make([]querycontract.EntityContent, 0, min(limit, s.entityCount))
	for i := 0; i < s.entityCount && i < limit; i++ {
		rows = append(rows, querycontract.EntityContent{
			EntityID: fmt.Sprintf("entity-%05d", i), RepoID: repoID, RelativePath: "svc/main.go",
			EntityType: "Function", EntityName: fmt.Sprintf("fn%05d", i), Language: "go",
		})
	}
	return rows, nil
}

func (s *semanticListStore) ListRepoFiles(_ context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	s.fileRequested.Store(true)
	s.fileLimit.Store(int32(limit))
	rows := make([]querycontract.FileContent, 0, min(limit, s.fileCount))
	for i := 0; i < s.fileCount && i < limit; i++ {
		rows = append(rows, querycontract.FileContent{
			RepoID: repoID, RelativePath: fmt.Sprintf("svc/file%05d.go", i), Language: "go", ContentHash: "h",
		})
	}
	return rows, nil
}

// TestGetRepositoryStoryDisclosesSemanticReadTruncation is the #7126
// truncation-marker proof. The story's semantic overview, file list, and every
// downstream consumer stay capped at RepositorySemanticEntityLimit; the read now
// asks for one more row as a sentinel and, only when that sentinel row exists,
// discloses the cap through the existing story truncation vocabulary
// (limitations, answer_metadata.partial_reasons, answer_metadata.truncated).
// At cap-1 and exactly cap the repository genuinely has that many rows, so no
// truncation may be claimed.
func TestGetRepositoryStoryDisclosesSemanticReadTruncation(t *testing.T) {
	t.Parallel()

	limit := querycontract.RepositorySemanticEntityLimit
	for _, tc := range []struct {
		name                   string
		entityCount, fileCount int
		wantTruncated          bool
	}{
		{"entities cap-1", limit - 1, 3, false},
		{"entities cap", limit, 3, false},
		{"entities cap+1", limit + 1, 3, true},
		{"files cap-1", 3, limit - 1, false},
		{"files cap", 3, limit, false},
		{"files cap+1", 3, limit + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &semanticListStore{
				ContentStore: NewContentReader(openContentReaderTestDB(t, nil)),
				entityCount:  tc.entityCount, fileCount: tc.fileCount,
			}
			body := getSemanticCapStoryBody(t, store)

			if got, want := int(store.entityLimit.Load()), limit+1; got != want {
				t.Fatalf("ListRepoEntities limit = %d, want %d (cap plus one sentinel row)", got, want)
			}
			if got, want := int(store.fileLimit.Load()), limit+1; got != want {
				t.Fatalf("ListRepoFiles limit = %d, want %d (cap plus one sentinel row)", got, want)
			}
			limitations, _ := body["limitations"].([]any)
			metadata, _ := body["answer_metadata"].(map[string]any)
			partial, _ := metadata["partial_reasons"].([]any)
			truncated, _ := metadata["truncated"].(bool)
			if got := testutil.AnySliceContains(limitations, repository.SemanticReadTruncatedReason); got != tc.wantTruncated {
				t.Fatalf("limitations = %#v contains %q = %v, want %v", limitations, repository.SemanticReadTruncatedReason, got, tc.wantTruncated)
			}
			if got := testutil.AnySliceContains(partial, repository.SemanticReadTruncatedReason); got != tc.wantTruncated {
				t.Fatalf("partial_reasons = %#v contains %q = %v, want %v", partial, repository.SemanticReadTruncatedReason, got, tc.wantTruncated)
			}
			if truncated != tc.wantTruncated {
				t.Fatalf("answer_metadata.truncated = %v, want %v", truncated, tc.wantTruncated)
			}
		})
	}
}

func getSemanticCapStoryBody(t *testing.T, store querycontract.ContentStore) map[string]any {
	t.Helper()
	handler := &RepositoryHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return map[string]any{"id": "repo-cap", "name": "repo-cap"}, nil
			},
		},
		Content: store,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-cap/story", nil)
	req.SetPathValue("repo_id", "repo-cap")
	rec := httptest.NewRecorder()
	handler.GetRepositoryStory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return body
}
