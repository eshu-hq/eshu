// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleLanguageQueryGuardFallbackFiltersBySemanticKindNotAllFunctions is
// the #5761 F2 regression. queryGraphFirstContentByLanguageWithSemanticFilter's
// content-store fallback (language_queries.go, reached whenever the graph
// read returns zero rows -- reachable in FULL profiles too, not only
// graphless ones) called queryContentByLanguage with the GRAPH label
// ("Function") instead of the "guard" content entity type, so it silently
// dropped the semantic_kind=guard filter and returned every Function in the
// language/repo instead of only guard clauses. contentEntityTypeFilter
// (elixir_semantic_types.go) already has the correct "guard" ->
// {entity_type=Function AND metadata->>semantic_kind=guard} clause; the bug
// was that the handler never asked for it.
//
// This test drives a graph reader that is non-nil and configured (unlike the
// F1 companion tests) but returns zero rows for the guard query, proving the
// fallback-dropped-filter defect fires on the ordinary
// graph-configured-but-empty path, not just the graphless one.
func TestHandleLanguageQueryGuardFallbackFiltersBySemanticKindNotAllFunctions(t *testing.T) {
	t.Parallel()

	content := &languageQueryContentStore{
		rows: []EntityContent{{
			EntityID:     "content-guard-1",
			RepoID:       "repo-1",
			RelativePath: "lib/foo.ex",
			EntityType:   "Function",
			EntityName:   "is_valid?",
			StartLine:    3,
			EndLine:      5,
			Language:     "elixir",
			Metadata:     map[string]any{"semantic_kind": "guard"},
		}},
	}
	handler := &LanguageQueryHandler{
		Neo4j:   &mockLanguageQueryGraphReader{rows: nil}, // configured, zero rows
		Content: content,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"elixir","entity_type":"guard","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := content.lastEntityType, "guard"; got != want {
		t.Fatalf("content entity type = %q, want %q (guard content fallback must filter by semantic_kind=guard, not return every Function)", got, want)
	}
}
