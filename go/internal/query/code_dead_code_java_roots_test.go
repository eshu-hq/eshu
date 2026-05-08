package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesJavaRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "java-main", "name": "main", "labels": []any{"Function"},
						"file_path": "src/main/java/example/CLI.java", "repo_id": "repo-1", "repo_name": "example", "language": "java",
						"dead_code_root_kinds": []any{"java.main_method"},
					},
					{
						"entity_id": "java-constructor", "name": "CLI", "labels": []any{"Function"},
						"file_path": "src/main/java/example/CLI.java", "repo_id": "repo-1", "repo_name": "example", "language": "java",
						"dead_code_root_kinds": []any{"java.constructor"},
					},
					{
						"entity_id": "java-override", "name": "close", "labels": []any{"Function"},
						"file_path": "src/main/java/example/CLI.java", "repo_id": "repo-1", "repo_name": "example", "language": "java",
						"dead_code_root_kinds": []any{"java.override_method"},
					},
					{
						"entity_id": "java-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "src/main/java/example/CLI.java", "repo_id": "repo-1", "repo_name": "example", "language": "java",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"java-helper": {
					EntityID:     "java-helper",
					RelativePath: "src/main/java/example/CLI.java",
					EntityType:   "Function",
					EntityName:   "helper",
					Language:     "java",
					SourceCache:  "private void helper() {}",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/dead-code", bytes.NewBufferString(`{"repo_id":"repo-1","limit":10}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	results := resp["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "java-helper"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
}
