package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesPHPRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "php-main", "name": "main", "labels": []any{"Function"},
						"file_path": "public/index.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
						"dead_code_root_kinds": []any{"php.script_entrypoint"},
					},
					{
						"entity_id": "php-controller", "name": "show", "labels": []any{"Function"},
						"file_path": "src/Controller/ReportController.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
						"dead_code_root_kinds": []any{"php.framework_controller_action"},
					},
					{
						"entity_id": "php-route", "name": "show", "labels": []any{"Function"},
						"file_path": "src/Controller/ReportController.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
						"dead_code_root_kinds": []any{"php.route_handler"},
					},
					{
						"entity_id": "php-symfony-route", "name": "detail", "labels": []any{"Function"},
						"file_path": "src/Controller/ReportController.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
						"dead_code_root_kinds": []any{"php.symfony_route_attribute"},
					},
					{
						"entity_id": "php-hook", "name": "wordpress_callback", "labels": []any{"Function"},
						"file_path": "wp-content/plugins/demo/plugin.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
						"dead_code_root_kinds": []any{"php.wordpress_hook_callback"},
					},
					{
						"entity_id": "php-interface", "name": "render", "labels": []any{"Function"},
						"file_path": "src/Reportable.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
						"dead_code_root_kinds": []any{"php.interface_method"},
					},
					{
						"entity_id": "php-interface-impl", "name": "render", "labels": []any{"Function"},
						"file_path": "src/ReportController.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
						"dead_code_root_kinds": []any{"php.interface_implementation_method"},
					},
					{
						"entity_id": "php-trait", "name": "bootAuditable", "labels": []any{"Function"},
						"file_path": "src/Auditable.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
						"dead_code_root_kinds": []any{"php.trait_method"},
					},
					{
						"entity_id": "php-constructor", "name": "__construct", "labels": []any{"Function"},
						"file_path": "src/ReportController.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
						"dead_code_root_kinds": []any{"php.constructor"},
					},
					{
						"entity_id": "php-magic", "name": "__invoke", "labels": []any{"Function"},
						"file_path": "src/ReportController.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
						"dead_code_root_kinds": []any{"php.magic_method"},
					},
					{
						"entity_id": "php-unused", "name": "unused_php_helper", "labels": []any{"Function"},
						"file_path": "src/helpers.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"php","limit":20}`),
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
	if got, want := result["entity_id"], "php-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}

	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(10); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	roots := analysis["modeled_framework_roots"].([]any)
	for _, rootKind := range phpDeadCodeMetadataRootKinds {
		if !queryTestStringSliceContains(roots, rootKind) {
			t.Fatalf("analysis[modeled_framework_roots] missing %q in %#v", rootKind, roots)
		}
	}
	maturity := analysis["dead_code_language_maturity"].(map[string]any)
	if got, want := maturity["php"], "derived"; got != want {
		t.Fatalf("dead_code_language_maturity[php] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesPHPRootKindsFromContentMetadata(t *testing.T) {
	t.Parallel()

	content := &contentCandidateDeadCodeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"php-main": {
					EntityID:     "php-main",
					RepoID:       "repo-1",
					RelativePath: "public/index.php",
					EntityType:   "Function",
					EntityName:   "main",
					Language:     "php",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"php.script_entrypoint"}},
				},
				"php-route": {
					EntityID:     "php-route",
					RepoID:       "repo-1",
					RelativePath: "src/Controller/ReportController.php",
					EntityType:   "Function",
					EntityName:   "show",
					Language:     "php",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"php.route_handler"}},
				},
				"php-unused": {
					EntityID:     "php-unused",
					RepoID:       "repo-1",
					RelativePath: "src/helpers.php",
					EntityType:   "Function",
					EntityName:   "unused_php_helper",
					Language:     "php",
				},
			},
		},
		rows: []map[string]any{
			{
				"entity_id": "php-main", "name": "main", "labels": []any{"Function"},
				"file_path": "public/index.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
			},
			{
				"entity_id": "php-route", "name": "show", "labels": []any{"Function"},
				"file_path": "src/Controller/ReportController.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
			},
			{
				"entity_id": "php-unused", "name": "unused_php_helper", "labels": []any{"Function"},
				"file_path": "src/helpers.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"php","limit":10}`),
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
	if got, want := result["entity_id"], "php-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
}
