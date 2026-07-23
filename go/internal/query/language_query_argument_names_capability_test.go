// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleLanguageQuery_FunctionMetadataSurfacesArgumentNames proves the
// symbol_graph.argument_names capability end to end through the real
// POST /api/v0/code/language-query handler. The parser emits a function's
// parameter names into the content-store fact payload (see
// pythonParameterNames in internal/parser/python/language.go, which fills
// the "args" key); enrichLanguageResultsWithContentMetadata
// (language_query_metadata.go) merges that content metadata onto the
// graph-backed function row. Existing coverage of this merge path
// (TestEnrichLanguageResultsWithContentMetadata,
// TestHandleLanguageQuery_TSXFunctionFragmentUsesGraphMetadataWithoutContent)
// asserts unrelated metadata fields (decorators, async, jsx flags) but never
// the argument-name payload the capability claims (issue #5681 cluster A).
func TestHandleLanguageQuery_FunctionMetadataSurfacesArgumentNames(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-process-payment-1",
				"name":       "process_payment",
				"labels":     []string{"Function"},
				"file_path":  "src/billing/payments.py",
				"repo_id":    "repo-1",
				"repo_name":  "repo-1",
				"language":   "python",
				"start_line": int64(12),
				"end_line":   int64(20),
			},
		}},
		Content: &languageQueryContentStore{rows: []EntityContent{
			{
				EntityID:     "content-process-payment-1",
				RepoID:       "repo-1",
				RelativePath: "src/billing/payments.py",
				EntityType:   "Function",
				EntityName:   "process_payment",
				StartLine:    12,
				EndLine:      20,
				Language:     "python",
				Metadata: map[string]any{
					"args": []any{"amount", "currency"},
				},
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"python","entity_type":"function","query":"process_payment","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed python function", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("result[metadata] type = %T, want map[string]any", result["metadata"])
	}
	args, ok := metadata["args"].([]any)
	if !ok {
		t.Fatalf("metadata[args] type = %T, want []any", metadata["args"])
	}
	if got, want := len(args), 2; got != want {
		t.Fatalf("len(metadata[args]) = %d, want %d: %#v", got, want, args)
	}
	if got, want := args[0], "amount"; got != want {
		t.Fatalf("metadata[args][0] = %#v, want %#v", got, want)
	}
	if got, want := args[1], "currency"; got != want {
		t.Fatalf("metadata[args][1] = %#v, want %#v", got, want)
	}
}
