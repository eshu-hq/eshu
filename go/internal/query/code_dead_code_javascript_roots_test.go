package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesJavaScriptFrameworkRootsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "js-next", "name": "GET", "labels": []any{"Function"},
						"file_path": "app/api/health/route.ts", "repo_id": "repo-1", "repo_name": "payments", "language": "typescript",
					},
					{
						"entity_id": "js-express", "name": "login", "labels": []any{"Function"},
						"file_path": "server/routes.ts", "repo_id": "repo-1", "repo_name": "payments", "language": "typescript",
					},
					{
						"entity_id": "js-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "server/helpers.ts", "repo_id": "repo-1", "repo_name": "payments", "language": "typescript",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"js-next": {
					EntityID:     "js-next",
					RelativePath: "app/api/health/route.ts",
					EntityType:   "Function",
					EntityName:   "GET",
					Language:     "typescript",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"javascript.nextjs_route_export"},
					},
				},
				"js-express": {
					EntityID:     "js-express",
					RelativePath: "server/routes.ts",
					EntityType:   "Function",
					EntityName:   "login",
					Language:     "typescript",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"javascript.express_route_registration"},
					},
				},
				"js-helper": {
					EntityID:     "js-helper",
					RelativePath: "server/helpers.ts",
					EntityType:   "Function",
					EntityName:   "helper",
					Language:     "typescript",
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
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	only, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("results[0] type = %T, want map[string]any", results[0])
	}
	if got, want := only["entity_id"], "js-helper"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}

	analysis, ok := resp["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", resp["analysis"])
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(2); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeJavaScriptRootsRemainDerivedMaturity(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "js-route", "name": "listUsers", "labels": []any{"Function"},
						"file_path": "server/routes.js", "repo_id": "repo-1", "repo_name": "payments", "language": "javascript",
					},
					{
						"entity_id": "js-unused", "name": "unusedLocalHelper", "labels": []any{"Function"},
						"file_path": "src/app.js", "repo_id": "repo-1", "repo_name": "payments", "language": "javascript",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"js-route": {
					EntityID:     "js-route",
					RelativePath: "server/routes.js",
					EntityType:   "Function",
					EntityName:   "listUsers",
					Language:     "javascript",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"javascript.express_route_registration"},
					},
				},
				"js-unused": {
					EntityID:     "js-unused",
					RelativePath: "src/app.js",
					EntityType:   "Function",
					EntityName:   "unusedLocalHelper",
					Language:     "javascript",
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
	truth, ok := resp["truth"].(map[string]any)
	if !ok {
		t.Fatalf("truth type = %T, want map[string]any", resp["truth"])
	}
	if got, want := truth["level"], string(TruthLevelDerived); got != want {
		t.Fatalf("truth[level] = %#v, want %#v", got, want)
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
	analysis, ok := data["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", data["analysis"])
	}
	maturity, ok := analysis["dead_code_language_maturity"].(map[string]any)
	if !ok {
		t.Fatalf("analysis[dead_code_language_maturity] type = %T, want map[string]any", analysis["dead_code_language_maturity"])
	}
	if got, want := maturity["javascript"], "derived"; got != want {
		t.Fatalf("dead_code_language_maturity[javascript] = %#v, want %#v", got, want)
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(1); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}
