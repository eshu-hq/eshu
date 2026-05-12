package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesHaskellRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	rootRows := []map[string]any{
		{"entity_id": "haskell-main", "name": "main", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"haskell.main_function"}},
		{"entity_id": "haskell-export", "name": "run", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"haskell.module_export"}},
		{"entity_id": "haskell-type", "name": "Worker", "labels": []any{"Class"}, "dead_code_root_kinds": []any{"haskell.exported_type"}},
		{"entity_id": "haskell-class-method", "name": "runTask", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"haskell.typeclass_method"}},
		{"entity_id": "haskell-instance-method", "name": "runTask", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"haskell.instance_method"}},
	}
	rows := make([]map[string]any, 0, len(rootRows)+1)
	for _, row := range rootRows {
		row["file_path"] = "src/Demo/App.hs"
		row["repo_id"] = "repo-1"
		row["repo_name"] = "demo"
		row["language"] = "haskell"
		rows = append(rows, row)
	}
	rows = append(rows, map[string]any{
		"entity_id": "haskell-unused", "name": "unusedCleanupCandidate", "labels": []any{"Function"},
		"file_path": "src/Demo/App.hs", "repo_id": "repo-1", "repo_name": "demo", "language": "haskell",
	})

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return rows, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"haskell","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp["data"].(map[string]any)
	results := data["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "haskell-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}

	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(len(rootRows)); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	roots := analysis["modeled_framework_roots"].([]any)
	for _, rootKind := range []string{
		"haskell.main_function",
		"haskell.module_export",
		"haskell.exported_type",
		"haskell.typeclass_method",
		"haskell.instance_method",
	} {
		if !queryTestStringSliceContains(roots, rootKind) {
			t.Fatalf("analysis[modeled_framework_roots] missing %q in %#v", rootKind, roots)
		}
	}
	maturity := analysis["dead_code_language_maturity"].(map[string]any)
	if got, want := maturity["haskell"], "derived"; got != want {
		t.Fatalf("dead_code_language_maturity[haskell] = %#v, want %#v", got, want)
	}
}
