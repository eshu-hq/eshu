package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesCPPRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "cpp-main", "name": "main", "labels": []any{"Function"},
						"file_path": "src/main.cpp", "repo_id": "repo-1", "repo_name": "runtime", "language": "cpp",
						"dead_code_root_kinds": []any{"cpp.main_function"},
					},
					{
						"entity_id": "cpp-public-api", "name": "Widget::render", "labels": []any{"Function"},
						"file_path": "src/widget.cpp", "repo_id": "repo-1", "repo_name": "runtime", "language": "cpp",
						"dead_code_root_kinds": []any{"cpp.public_header_api"},
					},
					{
						"entity_id": "cpp-virtual-method", "name": "Base::run", "labels": []any{"Function"},
						"file_path": "src/widget.cpp", "repo_id": "repo-1", "repo_name": "runtime", "language": "cpp",
						"dead_code_root_kinds": []any{"cpp.virtual_method"},
					},
					{
						"entity_id": "cpp-override-method", "name": "Derived::run", "labels": []any{"Function"},
						"file_path": "src/widget.cpp", "repo_id": "repo-1", "repo_name": "runtime", "language": "cpp",
						"dead_code_root_kinds": []any{"cpp.override_method"},
					},
					{
						"entity_id": "cpp-callback-target", "name": "on_event", "labels": []any{"Function"},
						"file_path": "src/callbacks.cpp", "repo_id": "repo-1", "repo_name": "runtime", "language": "cpp",
						"dead_code_root_kinds": []any{"cpp.callback_argument_target"},
					},
					{
						"entity_id": "cpp-function-pointer-target", "name": "dispatch_target", "labels": []any{"Function"},
						"file_path": "src/callbacks.cpp", "repo_id": "repo-1", "repo_name": "runtime", "language": "cpp",
						"dead_code_root_kinds": []any{"cpp.function_pointer_target"},
					},
					{
						"entity_id": "cpp-node-addon-entrypoint", "name": "NAPI_MODULE_INIT", "labels": []any{"Function"},
						"file_path": "src/addon.cc", "repo_id": "repo-1", "repo_name": "runtime", "language": "cpp",
						"dead_code_root_kinds": []any{"cpp.node_addon_entrypoint"},
					},
					{
						"entity_id": "cpp-unused", "name": "unused_cleanup_candidate", "labels": []any{"Function"},
						"file_path": "src/widget.cpp", "repo_id": "repo-1", "repo_name": "runtime", "language": "cpp",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"cpp","limit":10}`),
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
	if got, want := result["entity_id"], "cpp-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["classification"], "unused"; got != want {
		t.Fatalf("results[0][classification] = %#v, want %#v", got, want)
	}

	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(7); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	roots := analysis["modeled_framework_roots"].([]any)
	for _, rootKind := range []string{
		"cpp.main_function",
		"cpp.public_header_api",
		"cpp.virtual_method",
		"cpp.override_method",
		"cpp.callback_argument_target",
		"cpp.function_pointer_target",
		"cpp.node_addon_entrypoint",
	} {
		if !queryTestStringSliceContains(roots, rootKind) {
			t.Fatalf("analysis[modeled_framework_roots] missing %q in %#v", rootKind, roots)
		}
	}
	publicAPI := analysis["modeled_public_api"].([]any)
	if !queryTestStringSliceContains(publicAPI, "cpp.public_header_api") {
		t.Fatalf("analysis[modeled_public_api] missing cpp.public_header_api in %#v", publicAPI)
	}
	maturity := analysis["dead_code_language_maturity"].(map[string]any)
	if got, want := maturity["cpp"], "derived"; got != want {
		t.Fatalf("analysis[dead_code_language_maturity][cpp] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesCPPRootKindsFromContentMetadata(t *testing.T) {
	t.Parallel()

	content := &contentCandidateDeadCodeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"cpp-main": {
					EntityID:     "cpp-main",
					RepoID:       "repo-1",
					RelativePath: "src/main.cpp",
					EntityType:   "Function",
					EntityName:   "main",
					Language:     "cpp",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"cpp.main_function"}},
				},
				"cpp-function-pointer-target": {
					EntityID:     "cpp-function-pointer-target",
					RepoID:       "repo-1",
					RelativePath: "src/callbacks.cpp",
					EntityType:   "Function",
					EntityName:   "dispatch_target",
					Language:     "cpp",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"cpp.function_pointer_target"}},
				},
				"cpp-unused": {
					EntityID:     "cpp-unused",
					RepoID:       "repo-1",
					RelativePath: "src/widget.cpp",
					EntityType:   "Function",
					EntityName:   "unused_cleanup_candidate",
					Language:     "cpp",
				},
			},
		},
		rows: []map[string]any{
			{
				"entity_id": "cpp-main", "name": "main", "labels": []any{"Function"},
				"file_path": "src/main.cpp", "repo_id": "repo-1", "repo_name": "runtime", "language": "cpp",
			},
			{
				"entity_id": "cpp-function-pointer-target", "name": "dispatch_target", "labels": []any{"Function"},
				"file_path": "src/callbacks.cpp", "repo_id": "repo-1", "repo_name": "runtime", "language": "cpp",
			},
			{
				"entity_id": "cpp-unused", "name": "unused_cleanup_candidate", "labels": []any{"Function"},
				"file_path": "src/widget.cpp", "repo_id": "repo-1", "repo_name": "runtime", "language": "cpp",
			},
		},
	}
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return nil, nil
			},
		},
		Content: content,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"cpp","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := content.candidateRepoID, "repo-1"; got != want {
		t.Fatalf("content candidate repo id = %q, want %q", got, want)
	}
	if got, want := content.candidateLanguages[0], "cpp"; got != want {
		t.Fatalf("content candidate language = %q, want %q", got, want)
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
	if got, want := result["entity_id"], "cpp-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}

	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(2); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}
