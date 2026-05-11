package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesCRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "c-main", "name": "main", "labels": []any{"Function"},
						"file_path": "src/main.c", "repo_id": "repo-1", "repo_name": "runtime", "language": "c",
						"dead_code_root_kinds": []any{"c.main_function"},
					},
					{
						"entity_id": "c-signal-handler", "name": "registered_signal_handler", "labels": []any{"Function"},
						"file_path": "src/main.c", "repo_id": "repo-1", "repo_name": "runtime", "language": "c",
						"dead_code_root_kinds": []any{"c.signal_handler"},
					},
					{
						"entity_id": "c-public-api", "name": "eshu_c_public_api", "labels": []any{"Function"},
						"file_path": "src/main.c", "repo_id": "repo-1", "repo_name": "runtime", "language": "c",
						"dead_code_root_kinds": []any{"c.public_header_api"},
					},
					{
						"entity_id": "c-callback-target", "name": "local_handler", "labels": []any{"Function"},
						"file_path": "src/main.c", "repo_id": "repo-1", "repo_name": "runtime", "language": "c",
						"dead_code_root_kinds": []any{"c.callback_argument_target"},
					},
					{
						"entity_id": "c-dispatch-target", "name": "dispatch_target", "labels": []any{"Function"},
						"file_path": "src/main.c", "repo_id": "repo-1", "repo_name": "runtime", "language": "c",
						"dead_code_root_kinds": []any{"c.function_pointer_target"},
					},
					{
						"entity_id": "c-unused", "name": "unused_cleanup_candidate", "labels": []any{"Function"},
						"file_path": "src/main.c", "repo_id": "repo-1", "repo_name": "runtime", "language": "c",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"c","limit":10}`),
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
	if got, want := result["entity_id"], "c-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["classification"], "unused"; got != want {
		t.Fatalf("results[0][classification] = %#v, want %#v", got, want)
	}

	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(5); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	roots := analysis["modeled_framework_roots"].([]any)
	for _, rootKind := range []string{
		"c.main_function",
		"c.signal_handler",
		"c.public_header_api",
		"c.callback_argument_target",
		"c.function_pointer_target",
	} {
		if !queryTestStringSliceContains(roots, rootKind) {
			t.Fatalf("analysis[modeled_framework_roots] missing %q in %#v", rootKind, roots)
		}
	}
}

func TestHandleDeadCodeExcludesCRootKindsFromContentMetadata(t *testing.T) {
	t.Parallel()

	content := &contentCandidateDeadCodeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"c-main": {
					EntityID:     "c-main",
					RepoID:       "repo-1",
					RelativePath: "src/main.c",
					EntityType:   "Function",
					EntityName:   "main",
					Language:     "c",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"c.main_function"}},
				},
				"c-unused": {
					EntityID:     "c-unused",
					RepoID:       "repo-1",
					RelativePath: "src/main.c",
					EntityType:   "Function",
					EntityName:   "unused_cleanup_candidate",
					Language:     "c",
				},
			},
		},
		rows: []map[string]any{
			{
				"entity_id": "c-main", "name": "main", "labels": []any{"Function"},
				"file_path": "src/main.c", "repo_id": "repo-1", "repo_name": "runtime", "language": "c",
			},
			{
				"entity_id": "c-unused", "name": "unused_cleanup_candidate", "labels": []any{"Function"},
				"file_path": "src/main.c", "repo_id": "repo-1", "repo_name": "runtime", "language": "c",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"c","limit":10}`),
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
	if got, want := content.candidateLanguages[0], "c"; got != want {
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
	if got, want := result["entity_id"], "c-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}

	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(1); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}
