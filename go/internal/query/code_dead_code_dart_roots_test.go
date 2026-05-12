package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesDartRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	rootRows := []map[string]any{
		{"entity_id": "dart-main", "name": "main", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"dart.main_function"}},
		{"entity_id": "dart-constructor", "name": "DemoApp", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"dart.constructor"}},
		{"entity_id": "dart-override", "name": "build", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"dart.override_method"}},
		{"entity_id": "dart-build", "name": "build", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"dart.flutter_widget_build"}},
		{"entity_id": "dart-create-state", "name": "createState", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"dart.flutter_create_state"}},
		{"entity_id": "dart-public-api", "name": "PublicHelper", "labels": []any{"Class"}, "dead_code_root_kinds": []any{"dart.public_library_api"}},
	}
	rows := make([]map[string]any, 0, len(rootRows)+1)
	for _, row := range rootRows {
		row["file_path"] = "lib/home.dart"
		row["repo_id"] = "repo-1"
		row["repo_name"] = "dart-app"
		row["language"] = "dart"
		rows = append(rows, row)
	}
	rows = append(rows, map[string]any{
		"entity_id": "dart-unused", "name": "unusedCleanupCandidate", "labels": []any{"Function"},
		"file_path": "lib/src/internal.dart", "repo_id": "repo-1", "repo_name": "dart-app", "language": "dart",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"dart","limit":10}`),
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
	if got, want := result["entity_id"], "dart-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["classification"], "unused"; got != want {
		t.Fatalf("result[classification] = %#v, want %#v", got, want)
	}

	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(len(rootRows)); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	roots := analysis["modeled_framework_roots"].([]any)
	for _, rootKind := range []string{
		"dart.main_function",
		"dart.constructor",
		"dart.override_method",
		"dart.flutter_widget_build",
		"dart.flutter_create_state",
		"dart.public_library_api",
	} {
		if !queryTestStringSliceContains(roots, rootKind) {
			t.Fatalf("analysis[modeled_framework_roots] missing %q in %#v", rootKind, roots)
		}
	}
	maturity := analysis["dead_code_language_maturity"].(map[string]any)
	if got, want := maturity["dart"], "derived"; got != want {
		t.Fatalf("dead_code_language_maturity[dart] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesDartRootKindsFromContentMetadata(t *testing.T) {
	t.Parallel()

	content := &contentCandidateDeadCodeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"dart-main": {
					EntityID:     "dart-main",
					RepoID:       "repo-1",
					RelativePath: "lib/main.dart",
					EntityType:   "Function",
					EntityName:   "main",
					Language:     "dart",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"dart.main_function"}},
				},
				"dart-build": {
					EntityID:     "dart-build",
					RepoID:       "repo-1",
					RelativePath: "lib/home.dart",
					EntityType:   "Function",
					EntityName:   "build",
					Language:     "dart",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"dart.flutter_widget_build"}},
				},
				"dart-unused": {
					EntityID:     "dart-unused",
					RepoID:       "repo-1",
					RelativePath: "lib/src/internal.dart",
					EntityType:   "Function",
					EntityName:   "unusedCleanupCandidate",
					Language:     "dart",
				},
			},
		},
		rows: []map[string]any{
			{
				"entity_id": "dart-main", "name": "main", "labels": []any{"Function"},
				"file_path": "lib/main.dart", "repo_id": "repo-1", "repo_name": "dart-app", "language": "dart",
			},
			{
				"entity_id": "dart-build", "name": "build", "labels": []any{"Function"},
				"file_path": "lib/home.dart", "repo_id": "repo-1", "repo_name": "dart-app", "language": "dart",
			},
			{
				"entity_id": "dart-unused", "name": "unusedCleanupCandidate", "labels": []any{"Function"},
				"file_path": "lib/src/internal.dart", "repo_id": "repo-1", "repo_name": "dart-app", "language": "dart",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"dart","limit":10}`),
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
	if got, want := result["entity_id"], "dart-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(2); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
}
