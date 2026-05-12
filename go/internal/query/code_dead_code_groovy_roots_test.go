package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeadCodeExcludesGroovyJenkinsRootKindsFromMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "groovy-jenkinsfile", "name": "Jenkinsfile", "labels": []any{"Function"},
						"file_path": "Jenkinsfile", "repo_id": "repo-1", "repo_name": "pipeline-examples", "language": "groovy",
						"dead_code_root_kinds": []any{"groovy.jenkins_pipeline_entrypoint"},
					},
					{
						"entity_id": "groovy-vars-call", "name": "call", "labels": []any{"Function"},
						"file_path": "vars/deployService.groovy", "repo_id": "repo-1", "repo_name": "pipeline-examples", "language": "groovy",
						"dead_code_root_kinds": []any{"groovy.shared_library_call"},
					},
					{
						"entity_id": "groovy-helper", "name": "unusedHelper", "labels": []any{"Function"},
						"file_path": "src/DeployHelper.groovy", "repo_id": "repo-1", "repo_name": "pipeline-examples", "language": "groovy",
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
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"groovy","limit":10}`),
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
	if got, want := result["entity_id"], "groovy-helper"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["classification"], "derived_candidate_only"; got != want {
		t.Fatalf("results[0][classification] = %#v, want %#v", got, want)
	}

	analysis := data["analysis"].(map[string]any)
	if got, want := analysis["framework_roots_from_parser_metadata"], float64(2); got != want {
		t.Fatalf("analysis[framework_roots_from_parser_metadata] = %#v, want %#v", got, want)
	}
	roots := analysis["modeled_framework_roots"].([]any)
	for _, rootKind := range []string{
		"groovy.jenkins_pipeline_entrypoint",
		"groovy.shared_library_call",
	} {
		if !queryTestStringSliceContains(roots, rootKind) {
			t.Fatalf("analysis[modeled_framework_roots] missing %q in %#v", rootKind, roots)
		}
	}
	blockers := analysis["dead_code_language_exactness_blockers"].(map[string]any)
	groovyBlockers, ok := blockers["groovy"].([]any)
	if !ok {
		t.Fatalf("blockers[groovy] type = %T, want []any", blockers["groovy"])
	}
	for _, want := range []string{
		"dynamic_dispatch_unresolved",
		"closure_delegate_resolution_unavailable",
		"jenkins_shared_library_resolution_unavailable",
		"pipeline_dsl_dynamic_steps_unresolved",
	} {
		if !queryTestStringSliceContains(groovyBlockers, want) {
			t.Fatalf("blockers[groovy] missing %q in %#v", want, groovyBlockers)
		}
	}
}
