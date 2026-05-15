package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesAWSRuntimeDriftFindings(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/aws/runtime-drift/findings")
	post := mustMapField(t, path, "post")
	if got, want := post["operationId"], "listAWSRuntimeDriftFindings"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	responses := mustMapField(t, post, "responses")
	ok := mustMapField(t, responses, "200")
	content := mustMapField(t, ok, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	properties := mustMapField(t, schema, "properties")
	if _, ok := properties["drift_findings"]; !ok {
		t.Fatal("aws runtime drift response schema missing drift_findings")
	}
	if _, ok := properties["outcome_groups"]; !ok {
		t.Fatal("aws runtime drift response schema missing outcome_groups")
	}
}
