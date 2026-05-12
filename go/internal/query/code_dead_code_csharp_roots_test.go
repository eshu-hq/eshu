package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesCSharpRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "cs-main", "name": "Main", "labels": []any{"Function"},
						"file_path": "src/Program.cs", "repo_id": "repo-1", "repo_name": "dotnet-app", "language": "c_sharp",
						"dead_code_root_kinds": []any{"csharp.main_method"},
					},
					{
						"entity_id": "cs-controller", "name": "Get", "labels": []any{"Function"},
						"file_path": "src/PublicController.cs", "repo_id": "repo-1", "repo_name": "dotnet-app", "language": "c_sharp",
						"dead_code_root_kinds": []any{"csharp.aspnet_controller_action"},
					},
					{
						"entity_id": "cs-constructor", "name": "ReportJob", "labels": []any{"Function"},
						"file_path": "src/ReportJob.cs", "repo_id": "repo-1", "repo_name": "dotnet-app", "language": "c_sharp",
						"dead_code_root_kinds": []any{"csharp.constructor"},
					},
					{
						"entity_id": "cs-worker", "name": "ExecuteAsync", "labels": []any{"Function"},
						"file_path": "src/Worker.cs", "repo_id": "repo-1", "repo_name": "dotnet-app", "language": "c_sharp",
						"dead_code_root_kinds": []any{"csharp.hosted_service_entrypoint"},
					},
					{
						"entity_id": "cs-interface", "name": "Run", "labels": []any{"Function"},
						"file_path": "src/IJob.cs", "repo_id": "repo-1", "repo_name": "dotnet-app", "language": "c_sharp",
						"dead_code_root_kinds": []any{"csharp.interface_method"},
					},
					{
						"entity_id": "cs-impl", "name": "Run", "labels": []any{"Function"},
						"file_path": "src/ReportJob.cs", "repo_id": "repo-1", "repo_name": "dotnet-app", "language": "c_sharp",
						"dead_code_root_kinds": []any{"csharp.interface_implementation_method"},
					},
					{
						"entity_id": "cs-override", "name": "Execute", "labels": []any{"Function"},
						"file_path": "src/ScheduledJob.cs", "repo_id": "repo-1", "repo_name": "dotnet-app", "language": "c_sharp",
						"dead_code_root_kinds": []any{"csharp.override_method"},
					},
					{
						"entity_id": "cs-test", "name": "RunsFromTestRunner", "labels": []any{"Function"},
						"file_path": "tests/ServiceTests.cs", "repo_id": "repo-1", "repo_name": "dotnet-app", "language": "c_sharp",
						"dead_code_root_kinds": []any{"csharp.test_method"},
					},
					{
						"entity_id": "cs-serialization", "name": "Restore", "labels": []any{"Function"},
						"file_path": "src/SerializedState.cs", "repo_id": "repo-1", "repo_name": "dotnet-app", "language": "c_sharp",
						"dead_code_root_kinds": []any{"csharp.serialization_callback"},
					},
					{
						"entity_id": "cs-unused", "name": "UnusedCleanupCandidate", "labels": []any{"Function"},
						"file_path": "src/ReportJob.cs", "repo_id": "repo-1", "repo_name": "dotnet-app", "language": "c_sharp",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"csharp","limit":10}`),
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
	if got, want := data["language"], "c_sharp"; got != want {
		t.Fatalf("data[language] = %#v, want %#v", got, want)
	}
	results := data["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "cs-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["classification"], "unused"; got != want {
		t.Fatalf("results[0][classification] = %#v, want %#v", got, want)
	}

	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(9); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	roots := analysis["modeled_framework_roots"].([]any)
	for _, rootKind := range []string{
		"csharp.main_method",
		"csharp.constructor",
		"csharp.aspnet_controller_action",
		"csharp.hosted_service_entrypoint",
		"csharp.interface_method",
		"csharp.interface_implementation_method",
		"csharp.override_method",
		"csharp.test_method",
		"csharp.serialization_callback",
	} {
		if !queryTestStringSliceContains(roots, rootKind) {
			t.Fatalf("analysis[modeled_framework_roots] missing %q in %#v", rootKind, roots)
		}
	}
	maturity := analysis["dead_code_language_maturity"].(map[string]any)
	if got, want := maturity["c_sharp"], "derived"; got != want {
		t.Fatalf("dead_code_language_maturity[c_sharp] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeExcludesCSharpRootKindsFromContentMetadata(t *testing.T) {
	t.Parallel()

	content := &contentCandidateDeadCodeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"cs-main": {
					EntityID:     "cs-main",
					RepoID:       "repo-1",
					RelativePath: "src/Program.cs",
					EntityType:   "Function",
					EntityName:   "Main",
					Language:     "c_sharp",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"csharp.main_method"}},
				},
				"cs-controller": {
					EntityID:     "cs-controller",
					RepoID:       "repo-1",
					RelativePath: "src/PublicController.cs",
					EntityType:   "Function",
					EntityName:   "Get",
					Language:     "c_sharp",
					Metadata:     map[string]any{"dead_code_root_kinds": []string{"csharp.aspnet_controller_action"}},
				},
				"cs-unused": {
					EntityID:     "cs-unused",
					RepoID:       "repo-1",
					RelativePath: "src/ReportJob.cs",
					EntityType:   "Function",
					EntityName:   "UnusedCleanupCandidate",
					Language:     "c_sharp",
				},
			},
		},
		rows: []map[string]any{
			{
				"entity_id": "cs-main", "name": "Main", "labels": []any{"Function"},
				"file_path": "src/Program.cs", "repo_id": "repo-1", "repo_name": "dotnet-app", "language": "c_sharp",
			},
			{
				"entity_id": "cs-controller", "name": "Get", "labels": []any{"Function"},
				"file_path": "src/PublicController.cs", "repo_id": "repo-1", "repo_name": "dotnet-app", "language": "c_sharp",
			},
			{
				"entity_id": "cs-unused", "name": "UnusedCleanupCandidate", "labels": []any{"Function"},
				"file_path": "src/ReportJob.cs", "repo_id": "repo-1", "repo_name": "dotnet-app", "language": "c_sharp",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"csharp","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := content.candidateLanguages[0], "c_sharp"; got != want {
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
	if got, want := result["entity_id"], "cs-unused"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
}
