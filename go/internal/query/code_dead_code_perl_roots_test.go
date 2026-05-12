package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesPerlRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	rootRows := []map[string]any{
		{"entity_id": "perl-main", "name": "main", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"perl.script_entrypoint"}},
		{"entity_id": "perl-package", "name": "Controller", "labels": []any{"Class"}, "dead_code_root_kinds": []any{"perl.package_namespace"}},
		{"entity_id": "perl-export", "name": "public_action", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"perl.exported_subroutine"}},
		{"entity_id": "perl-constructor", "name": "new", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"perl.constructor"}},
		{"entity_id": "perl-begin", "name": "BEGIN", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"perl.special_block"}},
		{"entity_id": "perl-autoload", "name": "AUTOLOAD", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"perl.autoload_subroutine"}},
		{"entity_id": "perl-destroy", "name": "DESTROY", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"perl.destroy_subroutine"}},
	}
	rows := make([]map[string]any, 0, len(rootRows)+1)
	for _, row := range rootRows {
		row["file_path"] = "lib/App/Controller.pm"
		row["repo_id"] = "repo-1"
		row["repo_name"] = "perl-app"
		row["language"] = "perl"
		rows = append(rows, row)
	}
	rows = append(rows, map[string]any{
		"entity_id": "perl-unused", "name": "unused_cleanup_candidate", "labels": []any{"Function"},
		"file_path": "lib/App/Internal.pm", "repo_id": "repo-1", "repo_name": "perl-app", "language": "perl",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"perl","limit":10}`),
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
	if got, want := result["entity_id"], "perl-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}

	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(len(rootRows)); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	roots := analysis["modeled_framework_roots"].([]any)
	for _, rootKind := range []string{
		"perl.script_entrypoint",
		"perl.package_namespace",
		"perl.exported_subroutine",
		"perl.constructor",
		"perl.special_block",
		"perl.autoload_subroutine",
		"perl.destroy_subroutine",
	} {
		if !queryTestStringSliceContains(roots, rootKind) {
			t.Fatalf("analysis[modeled_framework_roots] missing %q in %#v", rootKind, roots)
		}
	}
	maturity := analysis["dead_code_language_maturity"].(map[string]any)
	if got, want := maturity["perl"], "derived"; got != want {
		t.Fatalf("dead_code_language_maturity[perl] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesPerlRootKindsFromContentMetadata(t *testing.T) {
	t.Parallel()

	content := &contentCandidateDeadCodeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"perl-main": {
					EntityID:     "perl-main",
					RepoID:       "repo-1",
					RelativePath: "script/app.pl",
					EntityType:   "Function",
					EntityName:   "main",
					Language:     "perl",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"perl.script_entrypoint"}},
				},
				"perl-export": {
					EntityID:     "perl-export",
					RepoID:       "repo-1",
					RelativePath: "lib/App.pm",
					EntityType:   "Function",
					EntityName:   "public_action",
					Language:     "perl",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"perl.exported_subroutine"}},
				},
				"perl-unused": {
					EntityID:     "perl-unused",
					RepoID:       "repo-1",
					RelativePath: "lib/App/Internal.pm",
					EntityType:   "Function",
					EntityName:   "unused_cleanup_candidate",
					Language:     "perl",
				},
			},
		},
		rows: []map[string]any{
			{
				"entity_id": "perl-main", "name": "main", "labels": []any{"Function"},
				"file_path": "script/app.pl", "repo_id": "repo-1", "repo_name": "perl-app", "language": "perl",
			},
			{
				"entity_id": "perl-export", "name": "public_action", "labels": []any{"Function"},
				"file_path": "lib/App.pm", "repo_id": "repo-1", "repo_name": "perl-app", "language": "perl",
			},
			{
				"entity_id": "perl-unused", "name": "unused_cleanup_candidate", "labels": []any{"Function"},
				"file_path": "lib/App/Internal.pm", "repo_id": "repo-1", "repo_name": "perl-app", "language": "perl",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"perl","limit":10}`),
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
	if got, want := result["entity_id"], "perl-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(2); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}
