// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleRelationshipsReturnsAmbiguousCandidatesWithoutGuessing(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				t.Fatal("relationships must not query graph when target resolution is ambiguous")
				return nil, nil
			},
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				t.Fatal("relationships must not query graph when target resolution is ambiguous")
				return nil, nil
			},
		},
		Content: resolvingContentStore{
			matches: []EntityContent{
				{
					EntityID:     "content-entity:one",
					RepoID:       "repo-1",
					RelativePath: "go/internal/query/one.go",
					EntityType:   "Function",
					EntityName:   "handleRelationships",
					Language:     "go",
					StartLine:    22,
					EndLine:      44,
				},
				{
					EntityID:     "content-entity:two",
					RepoID:       "repo-1",
					RelativePath: "go/internal/query/two.go",
					EntityType:   "Function",
					EntityName:   "handleRelationships",
					Language:     "go",
					StartLine:    55,
					EndLine:      77,
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"name":"handleRelationships","repo_id":"repo-1","direction":"outgoing","relationship_type":"CALLS"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["status"], "ambiguous"; got != want {
		t.Fatalf("status = %#v, want %#v", got, want)
	}
	resolution, ok := resp["target_resolution"].(map[string]any)
	if !ok {
		t.Fatalf("target_resolution type = %T, want map[string]any", resp["target_resolution"])
	}
	if got, want := resolution["status"], "ambiguous"; got != want {
		t.Fatalf("target_resolution.status = %#v, want %#v", got, want)
	}
	candidates, ok := resolution["candidates"].([]any)
	if !ok {
		t.Fatalf("target_resolution.candidates type = %T, want []any", resolution["candidates"])
	}
	if got, want := len(candidates), 2; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	first, ok := candidates[0].(map[string]any)
	if !ok {
		t.Fatalf("candidate type = %T, want map[string]any", candidates[0])
	}
	if got, want := first["handle"], "entity:content-entity:one"; got != want {
		t.Fatalf("candidate handle = %#v, want %#v", got, want)
	}
}
