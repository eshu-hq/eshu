package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeUsesParserRegistrationRootMetadataWithoutSourceCache(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"entity_id": "go-http-root", "name": "ServePayments", "labels": []any{"Function"},
						"file_path": "internal/http/payments.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "go-cli-root", "name": "runPayments", "labels": []any{"Function"},
						"file_path": "cmd/payments/root.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
					{
						"entity_id": "go-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "internal/payments/helper.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"go-http-root": {
					EntityID:     "go-http-root",
					RelativePath: "internal/http/payments.go",
					EntityType:   "Function",
					EntityName:   "ServePayments",
					Language:     "go",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"go.net_http_handler_registration"},
					},
				},
				"go-cli-root": {
					EntityID:     "go-cli-root",
					RelativePath: "cmd/payments/root.go",
					EntityType:   "Function",
					EntityName:   "runPayments",
					Language:     "go",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"go.cobra_run_registration"},
					},
				},
				"go-helper": {
					EntityID:     "go-helper",
					RelativePath: "internal/payments/helper.go",
					EntityType:   "Function",
					EntityName:   "helper",
					Language:     "go",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1"}`),
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
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", resp["data"])
	}
	results, ok := data["results"].([]any)
	if !ok {
		t.Fatalf("results type = %T, want []any", data["results"])
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "go-helper"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}

	analysis, ok := data["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", data["analysis"])
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(2); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	if got, want := analysis["framework_roots_from_source_fallback"], float64(0); got != want {
		t.Fatalf("analysis[framework_roots_from_source_fallback] = %#v, want %#v", got, want)
	}
}
