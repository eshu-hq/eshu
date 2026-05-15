package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesHardcodedSecretInvestigation(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/code/security/secrets/investigate")
	post := mustMapField(t, path, "post")
	body := mustMapField(t, post, "requestBody")
	content := mustMapField(t, body, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	properties := mustMapField(t, schema, "properties")
	if _, ok := properties["finding_kinds"]; !ok {
		t.Fatal("hardcoded secret request schema missing finding_kinds")
	}
	if _, ok := properties["include_suppressed"]; !ok {
		t.Fatal("hardcoded secret request schema missing include_suppressed")
	}
}
