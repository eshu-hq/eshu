package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesKotlinRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	rootRows := []map[string]any{
		{"entity_id": "kotlin-main", "name": "main", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.main_function"}},
		{"entity_id": "kotlin-constructor", "name": "constructor", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.constructor"}},
		{"entity_id": "kotlin-interface", "name": "Runner", "labels": []any{"Interface"}, "dead_code_root_kinds": []any{"kotlin.interface_type"}},
		{"entity_id": "kotlin-interface-method", "name": "run", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.interface_method"}},
		{"entity_id": "kotlin-impl-method", "name": "run", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.interface_implementation_method"}},
		{"entity_id": "kotlin-override", "name": "close", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.override_method"}},
		{"entity_id": "kotlin-gradle-apply", "name": "apply", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.gradle_plugin_apply"}},
		{"entity_id": "kotlin-gradle-action", "name": "execute", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.gradle_task_action"}},
		{"entity_id": "kotlin-gradle-property", "name": "getTarget", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.gradle_task_property"}},
		{"entity_id": "kotlin-gradle-setter", "name": "setEnabled", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.gradle_task_setter"}},
		{"entity_id": "kotlin-spring-component", "name": "GreetingController", "labels": []any{"Class"}, "dead_code_root_kinds": []any{"kotlin.spring_component_class"}},
		{"entity_id": "kotlin-spring-mapping", "name": "hello", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.spring_request_mapping_method"}},
		{"entity_id": "kotlin-spring-bean", "name": "client", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.spring_bean_method"}},
		{"entity_id": "kotlin-spring-scheduled", "name": "tick", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.spring_scheduled_method"}},
		{"entity_id": "kotlin-lifecycle", "name": "init", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.lifecycle_callback_method"}},
		{"entity_id": "kotlin-junit", "name": "runs", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"kotlin.junit_test_method"}},
	}
	rows := make([]map[string]any, 0, len(rootRows)+1)
	for _, row := range rootRows {
		row["file_path"] = "src/main/kotlin/example/App.kt"
		row["repo_id"] = "repo-1"
		row["repo_name"] = "example"
		row["language"] = "kotlin"
		rows = append(rows, row)
	}
	rows = append(rows, map[string]any{
		"entity_id": "kotlin-helper", "name": "unusedCleanupCandidate", "labels": []any{"Function"},
		"file_path": "src/main/kotlin/example/App.kt", "repo_id": "repo-1", "repo_name": "example", "language": "kotlin",
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

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/dead-code", bytes.NewBufferString(`{"repo_id":"repo-1","language":"kotlin","limit":10}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	results := resp["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "kotlin-helper"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["classification"], "unused"; got != want {
		t.Fatalf("result[classification] = %#v, want %#v", got, want)
	}
	analysis := resp["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(len(rootRows)); got != want {
		t.Fatalf("framework_roots_from_parser_metadata = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesKotlinRootKindsFromContentMetadata(t *testing.T) {
	t.Parallel()

	content := &contentCandidateDeadCodeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"kotlin-main": {
					EntityID:     "kotlin-main",
					RepoID:       "repo-1",
					RelativePath: "src/main/kotlin/example/App.kt",
					EntityType:   "Function",
					EntityName:   "main",
					Language:     "kotlin",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"kotlin.main_function"}},
				},
				"kotlin-spring": {
					EntityID:     "kotlin-spring",
					RepoID:       "repo-1",
					RelativePath: "src/main/kotlin/example/GreetingController.kt",
					EntityType:   "Function",
					EntityName:   "hello",
					Language:     "kotlin",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"kotlin.spring_request_mapping_method"}},
				},
				"kotlin-unused": {
					EntityID:     "kotlin-unused",
					RepoID:       "repo-1",
					RelativePath: "src/main/kotlin/example/App.kt",
					EntityType:   "Function",
					EntityName:   "unusedCleanupCandidate",
					Language:     "kotlin",
				},
			},
		},
		rows: []map[string]any{
			{
				"entity_id": "kotlin-main", "name": "main", "labels": []any{"Function"},
				"file_path": "src/main/kotlin/example/App.kt", "repo_id": "repo-1", "repo_name": "kotlin-app", "language": "kotlin",
			},
			{
				"entity_id": "kotlin-spring", "name": "hello", "labels": []any{"Function"},
				"file_path": "src/main/kotlin/example/GreetingController.kt", "repo_id": "repo-1", "repo_name": "kotlin-app", "language": "kotlin",
			},
			{
				"entity_id": "kotlin-unused", "name": "unusedCleanupCandidate", "labels": []any{"Function"},
				"file_path": "src/main/kotlin/example/App.kt", "repo_id": "repo-1", "repo_name": "kotlin-app", "language": "kotlin",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"kotlin","limit":10}`),
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
	if got, want := result["entity_id"], "kotlin-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["classification"], "unused"; got != want {
		t.Fatalf("result[classification] = %#v, want %#v", got, want)
	}
}
