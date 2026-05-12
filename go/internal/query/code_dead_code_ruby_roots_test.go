package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesRubyRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "ruby-controller-action", "name": "index", "labels": []any{"Function"},
						"file_path": "app/controllers/users_controller.rb", "repo_id": "repo-1", "repo_name": "rails-app", "language": "ruby",
						"dead_code_root_kinds": []any{"ruby.rails_controller_action"},
					},
					{
						"entity_id": "ruby-callback", "name": "authenticate_user!", "labels": []any{"Function"},
						"file_path": "app/controllers/users_controller.rb", "repo_id": "repo-1", "repo_name": "rails-app", "language": "ruby",
						"dead_code_root_kinds": []any{"ruby.rails_callback_method"},
					},
					{
						"entity_id": "ruby-dynamic-hook", "name": "method_missing", "labels": []any{"Function"},
						"file_path": "lib/dynamic_endpoint.rb", "repo_id": "repo-1", "repo_name": "rails-app", "language": "ruby",
						"dead_code_root_kinds": []any{"ruby.dynamic_dispatch_hook"},
					},
					{
						"entity_id": "ruby-method-reference", "name": "direct_helper", "labels": []any{"Function"},
						"file_path": "lib/worker.rb", "repo_id": "repo-1", "repo_name": "rails-app", "language": "ruby",
						"dead_code_root_kinds": []any{"ruby.method_reference_target"},
					},
					{
						"entity_id": "ruby-script-main", "name": "main", "labels": []any{"Function"},
						"file_path": "bin/sync", "repo_id": "repo-1", "repo_name": "rails-app", "language": "ruby",
						"dead_code_root_kinds": []any{"ruby.script_entrypoint"},
					},
					{
						"entity_id": "ruby-unused", "name": "unused_helper", "labels": []any{"Function"},
						"file_path": "lib/worker.rb", "repo_id": "repo-1", "repo_name": "rails-app", "language": "ruby",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"ruby","limit":10}`),
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
	if got, want := result["entity_id"], "ruby-unused"; got != want {
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
		"ruby.rails_controller_action",
		"ruby.rails_callback_method",
		"ruby.dynamic_dispatch_hook",
		"ruby.method_reference_target",
		"ruby.script_entrypoint",
	} {
		if !queryTestStringSliceContains(roots, rootKind) {
			t.Fatalf("analysis[modeled_framework_roots] missing %q in %#v", rootKind, roots)
		}
	}
}
