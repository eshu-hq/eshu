package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesScalaRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	rootRows := []map[string]any{
		{"entity_id": "scala-main", "name": "main", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"scala.main_method"}},
		{"entity_id": "scala-app-object", "name": "ConsoleApp", "labels": []any{"Class"}, "dead_code_root_kinds": []any{"scala.app_object"}},
		{"entity_id": "scala-trait", "name": "Runner", "labels": []any{"Trait"}, "dead_code_root_kinds": []any{"scala.trait_type"}},
		{"entity_id": "scala-trait-method", "name": "run", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"scala.trait_method"}},
		{"entity_id": "scala-impl-method", "name": "run", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"scala.trait_implementation_method"}},
		{"entity_id": "scala-override", "name": "receive", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"scala.override_method"}},
		{"entity_id": "scala-play", "name": "status", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"scala.play_controller_action"}},
		{"entity_id": "scala-akka", "name": "receive", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"scala.akka_actor_receive"}},
		{"entity_id": "scala-lifecycle", "name": "init", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"scala.lifecycle_callback_method"}},
		{"entity_id": "scala-junit", "name": "runsFromJUnit", "labels": []any{"Function"}, "dead_code_root_kinds": []any{"scala.junit_test_method"}},
		{"entity_id": "scala-scalatest", "name": "ServiceTests", "labels": []any{"Class"}, "dead_code_root_kinds": []any{"scala.scalatest_suite_class"}},
	}
	rows := make([]map[string]any, 0, len(rootRows)+1)
	for _, row := range rootRows {
		row["file_path"] = "src/main/scala/example/App.scala"
		row["repo_id"] = "repo-1"
		row["repo_name"] = "example"
		row["language"] = "scala"
		rows = append(rows, row)
	}
	rows = append(rows, map[string]any{
		"entity_id": "scala-helper", "name": "unusedCleanupCandidate", "labels": []any{"Function"},
		"file_path": "src/main/scala/example/App.scala", "repo_id": "repo-1", "repo_name": "example", "language": "scala",
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

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/dead-code", bytes.NewBufferString(`{"repo_id":"repo-1","language":"scala","limit":10}`))
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
	if got, want := result["entity_id"], "scala-helper"; got != want {
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

func TestHandleDeadCodeExcludesScalaRootKindsFromContentMetadata(t *testing.T) {
	t.Parallel()

	content := &contentCandidateDeadCodeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"scala-main": {
					EntityID:     "scala-main",
					RepoID:       "repo-1",
					RelativePath: "src/main/scala/example/App.scala",
					EntityType:   "Function",
					EntityName:   "main",
					Language:     "scala",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"scala.main_method"}},
				},
				"scala-play": {
					EntityID:     "scala-play",
					RepoID:       "repo-1",
					RelativePath: "src/main/scala/example/JobEndpoint.scala",
					EntityType:   "Function",
					EntityName:   "status",
					Language:     "scala",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"scala.play_controller_action"}},
				},
				"scala-unused": {
					EntityID:     "scala-unused",
					RepoID:       "repo-1",
					RelativePath: "src/main/scala/example/App.scala",
					EntityType:   "Function",
					EntityName:   "unusedCleanupCandidate",
					Language:     "scala",
				},
			},
		},
		rows: []map[string]any{
			{
				"entity_id": "scala-main", "name": "main", "labels": []any{"Function"},
				"file_path": "src/main/scala/example/App.scala", "repo_id": "repo-1", "repo_name": "scala-app", "language": "scala",
			},
			{
				"entity_id": "scala-play", "name": "status", "labels": []any{"Function"},
				"file_path": "src/main/scala/example/JobEndpoint.scala", "repo_id": "repo-1", "repo_name": "scala-app", "language": "scala",
			},
			{
				"entity_id": "scala-unused", "name": "unusedCleanupCandidate", "labels": []any{"Function"},
				"file_path": "src/main/scala/example/App.scala", "repo_id": "repo-1", "repo_name": "scala-app", "language": "scala",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"scala","limit":10}`),
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
	if got, want := result["entity_id"], "scala-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["classification"], "unused"; got != want {
		t.Fatalf("result[classification] = %#v, want %#v", got, want)
	}
}
