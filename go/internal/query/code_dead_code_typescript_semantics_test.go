package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestHandleDeadCodeReturnsGraphBackedTypeScriptSemantics(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if !strings.Contains(cypher, "e.decorators as decorators") {
					t.Fatalf("cypher = %q, want graph semantic projection", cypher)
				}
				return []map[string]any{
					{
						"entity_id":               "class-ts-1",
						"name":                    "Service",
						"labels":                  []any{"Class"},
						"file_path":               "src/service.ts",
						"repo_id":                 "repo-1",
						"repo_name":               "repo-1",
						"language":                "typescript",
						"start_line":              int64(1),
						"end_line":                int64(12),
						"decorators":              []any{"@sealed"},
						"type_parameters":         []any{"T"},
						"declaration_merge_group": "Service",
						"declaration_merge_count": int64(2),
						"declaration_merge_kinds": []any{"class", "namespace"},
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
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one graph-backed TypeScript result", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Class Service participates in TypeScript declaration merging with namespace Service."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	semantics, ok := result["typescript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("result[typescript_semantics] type = %T, want map[string]any", result["typescript_semantics"])
	}
	if got, want := semantics["decorators"], []any{"@sealed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typescript_semantics[decorators] = %#v, want %#v", got, want)
	}
	if got, want := semantics["type_parameters"], []any{"T"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typescript_semantics[type_parameters] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeTypeScriptAndTSXRootsRemainDerivedMaturity(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "ts-next", "name": "GET", "labels": []any{"Function"},
						"file_path": "app/api/accounts/route.ts", "repo_id": "repo-1", "repo_name": "payments", "language": "typescript",
					},
					{
						"entity_id": "tsx-next", "name": "POST", "labels": []any{"Function"},
						"file_path": "app/api/profile/route.tsx", "repo_id": "repo-1", "repo_name": "payments", "language": "tsx",
					},
					{
						"entity_id": "ts-unused", "name": "unusedTypedHelper", "labels": []any{"Function"},
						"file_path": "src/service.ts", "repo_id": "repo-1", "repo_name": "payments", "language": "typescript",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"ts-next": {
					EntityID:     "ts-next",
					RelativePath: "app/api/accounts/route.ts",
					EntityType:   "Function",
					EntityName:   "GET",
					Language:     "typescript",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"javascript.nextjs_route_export"},
					},
				},
				"tsx-next": {
					EntityID:     "tsx-next",
					RelativePath: "app/api/profile/route.tsx",
					EntityType:   "Function",
					EntityName:   "POST",
					Language:     "tsx",
					Metadata: map[string]any{
						"dead_code_root_kinds": []string{"javascript.nextjs_route_export"},
					},
				},
				"ts-unused": {
					EntityID:     "ts-unused",
					RelativePath: "src/service.ts",
					EntityType:   "Function",
					EntityName:   "unusedTypedHelper",
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
	if got, want := maturity["typescript"], "derived"; got != want {
		t.Fatalf("dead_code_language_maturity[typescript] = %#v, want %#v", got, want)
	}
	if got, want := maturity["tsx"], "derived"; got != want {
		t.Fatalf("dead_code_language_maturity[tsx] = %#v, want %#v", got, want)
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(2); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeSuppressesTypeScriptPublicAPIRootsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "ts-reexport", "name": "createClient", "labels": []any{"Function"},
						"file_path": "src/client.ts", "repo_id": "repo-1", "repo_name": "client-lib", "language": "typescript",
					},
					{
						"entity_id": "ts-type-reference", "name": "ClientOptions", "labels": []any{"Interface"},
						"file_path": "src/client.ts", "repo_id": "repo-1", "repo_name": "client-lib", "language": "typescript",
					},
					{
						"entity_id": "ts-unused", "name": "normalizeOptions", "labels": []any{"Function"},
						"file_path": "src/client.ts", "repo_id": "repo-1", "repo_name": "client-lib", "language": "typescript",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"ts-reexport": {
					EntityID: "ts-reexport", RelativePath: "src/client.ts", EntityType: "Function",
					EntityName: "createClient", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"typescript.public_api_reexport"}},
				},
				"ts-type-reference": {
					EntityID: "ts-type-reference", RelativePath: "src/client.ts", EntityType: "Interface",
					EntityName: "ClientOptions", Language: "typescript",
					Metadata: map[string]any{"dead_code_root_kinds": []string{"typescript.public_api_type_reference"}},
				},
				"ts-unused": {
					EntityID: "ts-unused", RelativePath: "src/client.ts", EntityType: "Function",
					EntityName: "normalizeOptions", Language: "typescript",
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
	if got, want := only["entity_id"], "ts-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}

	analysis, ok := resp["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis type = %T, want map[string]any", resp["analysis"])
	}
	modeledPublicAPI, ok := analysis["modeled_public_api"].([]any)
	if !ok {
		t.Fatalf("analysis[modeled_public_api] type = %T, want []any", analysis["modeled_public_api"])
	}
	for _, want := range []string{"typescript.public_api_reexport", "typescript.public_api_type_reference"} {
		if !queryTestStringSliceContains(modeledPublicAPI, want) {
			t.Fatalf("analysis[modeled_public_api] missing %s in %#v", want, modeledPublicAPI)
		}
	}
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(2); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}
