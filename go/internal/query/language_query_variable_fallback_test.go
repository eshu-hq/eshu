// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// TestHandleLanguageQueryVariableIncludesContentVariablesWhenGraphHasSemanticRow
// is a regression test for the P1 accuracy defect where moving "variable" into
// graphFirstContentBackedEntityTypes made queryGraphFirstContentByLanguageWithSemanticFilter
// skip the content store whenever Neo4j returned any rows at all. The reducer's
// canonical-graph skip (see canonical_builder.go) removes plain Variable graph
// nodes but a FEW semantic Variable graph nodes still exist (module attributes,
// TSX/Elixir component assertions). Those few non-empty graph rows previously
// short-circuited the content fallback, silently omitting the many plain
// variables that only live in the content store. Route "variable" to pure
// content-backed (see contentBackedEntityTypes) so this can't happen.
func TestHandleLanguageQueryVariableIncludesContentVariablesWhenGraphHasSemanticRow(t *testing.T) {
	t.Parallel()

	content := &languageQueryContentStore{
		rows: []EntityContent{
			{
				EntityID:     "content-variable-1",
				RepoID:       "repo-1",
				RelativePath: "src/config.ts",
				EntityType:   "Variable",
				EntityName:   "config",
				StartLine:    7,
				EndLine:      7,
				Language:     "typescript",
				Metadata:     map[string]any{"source": "content"},
			},
			{
				EntityID:     "content-variable-2",
				RepoID:       "repo-1",
				RelativePath: "src/other.ts",
				EntityType:   "Variable",
				EntityName:   "otherConfig",
				StartLine:    12,
				EndLine:      12,
				Language:     "typescript",
				Metadata:     map[string]any{"source": "content"},
			},
		},
	}
	// The graph mock returns exactly one semantic Variable row (e.g. a module
	// attribute or TSX component-assertion variable the reducer still writes
	// to the graph). Before the fix, this single non-empty graph result made
	// queryGraphFirstContentByLanguageWithSemanticFilter return early and
	// never call the content store for the plain variables above.
	graph := &mockLanguageQueryGraphReader{
		rows: []map[string]any{
			{
				"entity_id": "graph-variable-1",
				"name":      "semanticConfig",
				"labels":    []string{"Variable"},
				"file_path": "src/semantic.ts",
				"repo_id":   "repo-1",
				"language":  "typescript",
			},
		},
	}
	handler := &LanguageQueryHandler{
		Neo4j:   graph,
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
	if !ok {
		t.Fatalf("results = %#v, want []any", resp["results"])
	}
	if got, want := len(results), 2; got != want {
		t.Fatalf("len(results) = %d, want %d (plain variables must not be omitted): %#v", got, want, results)
	}
	gotIDs := make(map[string]bool, len(results))
	for _, r := range results {
		result, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("result type = %T, want map[string]any", r)
		}
		gotIDs[fmt.Sprint(result["entity_id"])] = true
	}
	for _, wantID := range []string{"content-variable-1", "content-variable-2"} {
		if !gotIDs[wantID] {
			t.Fatalf("results missing content-backed variable %q; got IDs=%v", wantID, gotIDs)
		}
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
