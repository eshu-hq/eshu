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

func TestHandleLanguageQueryVariableFallsBackToContentStore(t *testing.T) {
	t.Parallel()

	content := &languageQueryContentStore{
		rows: []EntityContent{{
			EntityID:     "content-variable-1",
			RepoID:       "repo-1",
			RelativePath: "src/config.ts",
			EntityType:   "Variable",
			EntityName:   "config",
			StartLine:    7,
			EndLine:      7,
			Language:     "typescript",
			Metadata:     map[string]any{"source": "content"},
		}},
	}
	handler := &LanguageQueryHandler{
		Neo4j:   &mockLanguageQueryGraphReader{},
		Content: content,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"typescript","entity_type":"variable","query":"config","repo_id":"repo-1"}`),
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
	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one content-backed variable", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "content-variable-1"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := content.lastEntityType, "Variable"; got != want {
		t.Fatalf("content entity type = %q, want %q", got, want)
	}
}

type languageQueryContentStore struct {
	fakePortContentStore
	rows           []EntityContent
	lastEntityType string
}

func (s *languageQueryContentStore) SearchEntitiesByLanguageAndType(
	_ context.Context,
	_ string,
	_ string,
	entityType string,
	_ string,
	_ int,
) ([]EntityContent, error) {
	s.lastEntityType = entityType
	return append([]EntityContent(nil), s.rows...), nil
}
