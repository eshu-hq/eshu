// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestContentHandlerSearchEntitiesUsesAnyRepoWhenRepoScopeOmitted(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"entity-2", "repo-2", "src/app.ts", "Function", "renderApp",
					int64(10), int64(20), "typescript", "function renderApp() {}", []byte(`{"kind":"handler"}`),
				},
			},
		},
	})

	handler := &ContentHandler{Content: NewContentReader(db)}
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

	if len(recorder.args) != 1 {
		t.Fatalf("len(recorder.args) = %d, want 1", len(recorder.args))
	}
	if got, want := len(recorder.args[0]), 3; got != want {
		t.Fatalf("len(query args) = %d, want %d", got, want)
	}
	if got, want := recorder.args[0][0], "renderApp"; got != want {
		t.Fatalf("query arg pattern = %#v, want %#v", got, want)
	}
	if got, want := numericDriverValue(t, recorder.args[0][1]), int64(11); got != want {
		t.Fatalf("query arg limit = %d, want %d", got, want)
	}
	if got, want := numericDriverValue(t, recorder.args[0][2]), int64(0); got != want {
		t.Fatalf("query arg offset = %d, want %d", got, want)
	}
	if strings.Contains(recorder.queries[0], "repo_id =") {
		t.Fatalf("query = %q, want any-repo search without repo filter", recorder.queries[0])
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("response count = %d, want %d", got, want)
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
