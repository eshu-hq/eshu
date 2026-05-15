package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPIImportDependencyInvestigation(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	importDependencyPath := mustMapField(t, paths, "/api/v0/code/imports/investigate")
	importDependencyPost := mustMapField(t, importDependencyPath, "post")
	importDependencyBody := mustMapField(t, mustMapField(t, importDependencyPost, "requestBody"), "content")
	importDependencyJSON := mustMapField(t, importDependencyBody, "application/json")
	importDependencyRequest := mustMapField(t, mustMapField(t, importDependencyJSON, "schema"), "properties")
	for _, field := range []string{"query_type", "repo_id", "language", "source_file", "target_file", "source_module", "target_module", "limit", "offset"} {
		if _, ok := importDependencyRequest[field]; !ok {
			t.Fatalf("code/imports/investigate request schema missing %s", field)
		}
	}
	importDependencyResponses := mustMapField(t, importDependencyPost, "responses")
	importDependencyOK := mustMapField(t, importDependencyResponses, "200")
	importDependencyContent := mustMapField(t, mustMapField(t, importDependencyOK, "content"), "application/json")
	importDependencyResponse := mustMapField(t, mustMapField(t, importDependencyContent, "schema"), "properties")
	for _, field := range []string{"results", "dependencies", "modules", "cycles", "cross_module_calls", "truncated", "next_offset", "source_backend", "coverage"} {
		if _, ok := importDependencyResponse[field]; !ok {
			t.Fatalf("code/imports/investigate response schema missing %s", field)
		}
	}
}
