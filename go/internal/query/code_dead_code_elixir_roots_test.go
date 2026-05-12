package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesElixirRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	rootRows := []map[string]any{
		{"entity_id": "elixir-start", "name": "start", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"elixir.application_start"}},
		{"entity_id": "elixir-macro", "name": "expose", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"elixir.public_macro"}},
		{"entity_id": "elixir-guard", "name": "is_even", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"elixir.public_guard"}},
		{"entity_id": "elixir-callback", "name": "init", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"elixir.behaviour_callback"}},
		{"entity_id": "elixir-genserver", "name": "handle_call", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"elixir.genserver_callback"}},
		{"entity_id": "elixir-mix", "name": "run", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"elixir.mix_task_run"}},
		{"entity_id": "elixir-protocol", "name": "serialize", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"elixir.protocol_function"}},
		{"entity_id": "elixir-protocol-impl", "name": "serialize", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"elixir.protocol_implementation_function"}},
		{"entity_id": "elixir-controller", "name": "index", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"elixir.phoenix_controller_action"}},
		{"entity_id": "elixir-liveview", "name": "mount", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"elixir.phoenix_liveview_callback"}},
	}
	rows := make([]map[string]any, 0, len(rootRows)+1)
	for _, row := range rootRows {
		row["file_path"] = "lib/demo_web/page_controller.ex"
		row["repo_id"] = "repo-1"
		row["repo_name"] = "demo"
		row["language"] = "elixir"
		rows = append(rows, row)
	}
	rows = append(rows, map[string]any{
		"entity_id": "elixir-unused", "name": "unused_cleanup_candidate", "labels": []any{"Function"},
		"file_path": "lib/demo/helper.ex", "repo_id": "repo-1", "repo_name": "demo", "language": "elixir",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"elixir","limit":20}`),
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
	if got, want := result["entity_id"], "elixir-unused"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["classification"], "unused"; got != want {
		t.Fatalf("result[classification] = %#v, want %#v", got, want)
	}
	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(len(rootRows)); got != want {
		t.Fatalf("framework_roots_from_parser_metadata = %#v, want %#v", got, want)
	}
	roots := analysis["modeled_framework_roots"].([]any)
	for _, rootKind := range elixirDeadCodeMetadataRootKinds {
		if !queryTestStringSliceContains(roots, rootKind) {
			t.Fatalf("analysis[modeled_framework_roots] missing %q in %#v", rootKind, roots)
		}
	}
	maturity := analysis["dead_code_language_maturity"].(map[string]any)
	if got, want := maturity["elixir"], "derived"; got != want {
		t.Fatalf("dead_code_language_maturity[elixir] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesElixirRootKindsFromContentMetadata(t *testing.T) {
	t.Parallel()

	content := &contentCandidateDeadCodeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"elixir-start": {
					EntityID:     "elixir-start",
					RepoID:       "repo-1",
					RelativePath: "lib/demo/application.ex",
					EntityType:   "Function",
					EntityName:   "start",
					Language:     "elixir",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"elixir.application_start"}},
				},
				"elixir-controller": {
					EntityID:     "elixir-controller",
					RepoID:       "repo-1",
					RelativePath: "lib/demo_web/page_controller.ex",
					EntityType:   "Function",
					EntityName:   "index",
					Language:     "elixir",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"elixir.phoenix_controller_action"}},
				},
				"elixir-unused": {
					EntityID:     "elixir-unused",
					RepoID:       "repo-1",
					RelativePath: "lib/demo/helper.ex",
					EntityType:   "Function",
					EntityName:   "unused_cleanup_candidate",
					Language:     "elixir",
				},
			},
		},
		rows: []map[string]any{
			{
				"entity_id": "elixir-start", "name": "start", "labels": []any{"Function"},
				"file_path": "lib/demo/application.ex", "repo_id": "repo-1", "repo_name": "demo", "language": "elixir",
			},
			{
				"entity_id": "elixir-controller", "name": "index", "labels": []any{"Function"},
				"file_path": "lib/demo_web/page_controller.ex", "repo_id": "repo-1", "repo_name": "demo", "language": "elixir",
			},
			{
				"entity_id": "elixir-unused", "name": "unused_cleanup_candidate", "labels": []any{"Function"},
				"file_path": "lib/demo/helper.ex", "repo_id": "repo-1", "repo_name": "demo", "language": "elixir",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"elixir","limit":10}`),
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
	if got, want := result["entity_id"], "elixir-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
}
