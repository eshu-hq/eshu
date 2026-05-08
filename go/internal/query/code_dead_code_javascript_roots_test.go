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

func TestHandleDeadCodeExcludesJavaScriptNodeAndHapiRootsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "js-entry", "name": "bootstrap", "labels": []any{"Function"},
						"file_path": "service-sample.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-bin", "name": "runCli", "labels": []any{"Function"},
						"file_path": "cli.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-export", "name": "publicApi", "labels": []any{"Function"},
						"file_path": "server/public-api.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-export-class", "name": "PublicClient", "labels": []any{"Class"},
						"file_path": "server/public-api.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-hapi", "name": "post", "labels": []any{"Function"},
						"file_path": "server/handlers/chat/response.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
					{
						"entity_id": "js-helper", "name": "unusedHelper", "labels": []any{"Function"},
						"file_path": "server/private-helper.ts", "repo_id": "repo-1", "repo_name": "service-sample", "language": "typescript",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"js-entry": {
					EntityID: "js-entry", RelativePath: "service-sample.ts", EntityType: "Function", EntityName: "bootstrap", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.node_package_entrypoint"}},
				},
				"js-bin": {
					EntityID: "js-bin", RelativePath: "cli.ts", EntityType: "Function", EntityName: "runCli", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.node_package_bin"}},
				},
				"js-export": {
					EntityID: "js-export", RelativePath: "server/public-api.ts", EntityType: "Function", EntityName: "publicApi", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.node_package_export"}},
				},
				"js-export-class": {
					EntityID: "js-export-class", RelativePath: "server/public-api.ts", EntityType: "Class", EntityName: "PublicClient", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.node_package_export"}},
				},
				"js-hapi": {
					EntityID: "js-hapi", RelativePath: "server/handlers/chat/response.ts", EntityType: "Function", EntityName: "post", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"javascript.hapi_handler_export"}},
				},
				"js-helper": {
					EntityID: "js-helper", RelativePath: "server/private-helper.ts", EntityType: "Function", EntityName: "unusedHelper", Language: "typescript",
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
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(5); got != want {
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
