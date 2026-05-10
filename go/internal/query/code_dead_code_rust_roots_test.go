package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesRustRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "rust-main", "name": "main", "labels": []any{"Function"},
						"file_path": "src/main.rs", "repo_id": "repo-1", "repo_name": "runtime", "language": "rust",
						"dead_code_root_kinds": []any{"rust.main_function"},
					},
					{
						"entity_id": "rust-tokio-main", "name": "main", "labels": []any{"Function"},
						"file_path": "src/main.rs", "repo_id": "repo-1", "repo_name": "runtime", "language": "rust",
						"dead_code_root_kinds": []any{"rust.tokio_main"},
					},
					{
						"entity_id": "rust-test", "name": "sync_smoke", "labels": []any{"Function"},
						"file_path": "src/lib.rs", "repo_id": "repo-1", "repo_name": "runtime", "language": "rust",
						"dead_code_root_kinds": []any{"rust.test_function"},
					},
					{
						"entity_id": "rust-tokio-test", "name": "async_smoke", "labels": []any{"Function"},
						"file_path": "src/lib.rs", "repo_id": "repo-1", "repo_name": "runtime", "language": "rust",
						"dead_code_root_kinds": []any{"rust.tokio_test"},
					},
					{
						"entity_id": "rust-public-api-item", "name": "parse_config", "labels": []any{"Function"},
						"file_path": "src/lib.rs", "repo_id": "repo-1", "repo_name": "runtime", "language": "rust",
						"dead_code_root_kinds": []any{"rust.public_api_item"},
					},
					{
						"entity_id": "rust-trait-impl-method", "name": "fmt", "labels": []any{"Function"},
						"file_path": "src/lib.rs", "repo_id": "repo-1", "repo_name": "runtime", "language": "rust",
						"dead_code_root_kinds": []any{"rust.trait_impl_method"},
					},
					{
						"entity_id": "rust-benchmark-function", "name": "bench_parser", "labels": []any{"Function"},
						"file_path": "benches/parser.rs", "repo_id": "repo-1", "repo_name": "runtime", "language": "rust",
						"dead_code_root_kinds": []any{"rust.benchmark_function"},
					},
					{
						"entity_id": "rust-benchmark-helper", "name": "helper_method", "labels": []any{"Function"},
						"file_path": "benches/copy.rs", "repo_id": "repo-1", "repo_name": "runtime", "language": "rust",
					},
					{
						"entity_id": "rust-example-helper", "name": "example_helper", "labels": []any{"Function"},
						"file_path": "examples/demo.rs", "repo_id": "repo-1", "repo_name": "runtime", "language": "rust",
					},
					{
						"entity_id": "rust-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "src/lib.rs", "repo_id": "repo-1", "repo_name": "runtime", "language": "rust",
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":10}`),
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
		t.Fatalf("results type = %T, want []any", resp["results"])
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("results[0] type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "rust-helper"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}

	analysis, ok := resp["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", resp["analysis"])
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(7); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	roots, ok := analysis["modeled_framework_roots"].([]any)
	if !ok {
		t.Fatalf("analysis[modeled_framework_roots] type = %T, want []any", analysis["modeled_framework_roots"])
	}
	for _, rootKind := range []string{
		"rust.main_function",
		"rust.test_function",
		"rust.tokio_main",
		"rust.tokio_test",
		"rust.public_api_item",
		"rust.trait_impl_method",
		"rust.benchmark_function",
	} {
		if !queryTestStringSliceContains(roots, rootKind) {
			t.Fatalf("analysis[modeled_framework_roots] missing %q in %#v", rootKind, roots)
		}
	}
}

func queryTestStringSliceContains(values []any, want string) bool {
	for _, value := range values {
		if got, ok := value.(string); ok && got == want {
			return true
		}
	}
	return false
}
