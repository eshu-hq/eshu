// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

func TestContentHandlerSearchFilesOmitsRemovedMatchesAlias(t *testing.T) {
	t.Parallel()

	db, _ := openRecordingContentSearchDB(t, []contentSearchQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-1", "src/app.ts", "", "",
					"hash-1", int64(24), "typescript", "source",
				},
			},
		},
	})

	handler := &ContentHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"pattern":"renderApp","repo_ids":["repo-1"],"limit":10}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one result", resp["results"])
	}
	if _, ok := resp["matches"]; ok {
		t.Fatalf("matches present, want the alias removed (#7170); body = %s", w.Body.String())
	}
	if got, want := resp["source_backend"], "postgres_content_store"; got != want {
		t.Fatalf("source_backend = %#v, want %#v", got, want)
	}
}

// anyRepoEntitySearchStore serves the bounded unscoped entity seam from a
// canned page: an omitted repo scope reaches the walk, not a per-request
// search loop.
type anyRepoEntitySearchStore struct {
	content.FakePortContentStore
	page                querycontract.EntitySearchPage
	gotPattern          string
	gotLimit, gotOffset int
	gotCursor           querycontract.SearchCursor
}

func (s *anyRepoEntitySearchStore) SearchEntitiesUnscoped(
	_ context.Context, pattern string, limit, offset int, cursor querycontract.SearchCursor,
) (querycontract.EntitySearchPage, error) {
	s.gotPattern, s.gotLimit, s.gotOffset, s.gotCursor = pattern, limit, offset, cursor
	return s.page, nil
}

func TestContentHandlerSearchEntitiesUsesAnyRepoWhenRepoScopeOmitted(t *testing.T) {
	t.Parallel()

	store := &anyRepoEntitySearchStore{page: querycontract.EntitySearchPage{
		Entities: []querycontract.EntityContent{{
			EntityID: "entity-2", RepoID: "repo-2", RelativePath: "src/app.ts",
			EntityType: "Function", EntityName: "renderApp",
			StartLine: 10, EndLine: 20, Language: "typescript",
			SourceCache: "function renderApp() {}",
			Metadata:    map[string]any{"kind": "handler"},
		}},
	}}
	handler := &ContentHandler{Content: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/entities/search",
		bytes.NewBufferString(`{"pattern":"renderApp","limit":10}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	if store.gotPattern != "renderApp" || store.gotLimit != 10 || store.gotOffset != 0 ||
		!store.gotCursor.IsZero() {
		t.Fatalf("seam args = %+v, want the pattern, limit 10, offset 0 and the zero cursor",
			[]any{store.gotPattern, store.gotLimit, store.gotOffset, store.gotCursor})
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("response count = %d, want %d", got, want)
	}
	if truncated, _ := resp["truncated"].(bool); truncated {
		t.Fatalf("response truncated = true, want the complete page; body = %s", w.Body.String())
	}
	if _, present := resp["partial"]; present {
		t.Fatalf("response carries partial, want an exact answer; body = %s", w.Body.String())
	}
	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("resp[results] = %#v, want one any-repo entity result", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[results][0] type = %T, want map[string]any", results[0])
	}
	if got, want := result["repo_id"], "repo-2"; got != want {
		t.Fatalf("result[repo_id] = %#v, want %#v", got, want)
	}
}
