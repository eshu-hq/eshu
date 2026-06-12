package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPIRelationshipStoryRestrictsTargetlessOverrides(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}

	paths := mustMapField(t, spec, "paths")
	relationshipStoryPath := mustMapField(t, paths, "/api/v0/code/relationships/story")
	relationshipStoryPost := mustMapField(t, relationshipStoryPath, "post")
	relationshipStoryBody := mustMapField(t, mustMapField(t, relationshipStoryPost, "requestBody"), "content")
	relationshipStoryJSON := mustMapField(t, relationshipStoryBody, "application/json")
	relationshipStoryRequestSchema := mustMapField(t, relationshipStoryJSON, "schema")
	anyOf, ok := relationshipStoryRequestSchema["anyOf"].([]any)
	if !ok || len(anyOf) != 3 {
		t.Fatalf("code/relationships/story anyOf = %#v, want three request branches", relationshipStoryRequestSchema["anyOf"])
	}
	overrideBranch := anyOf[2].(map[string]any)
	overrideProperties := mustMapField(t, overrideBranch, "properties")
	overrideQueryType := mustMapField(t, overrideProperties, "query_type")
	if !containsValue(overrideQueryType["enum"].([]any), "overrides") {
		t.Fatalf("targetless query_type+repo_id branch enum = %#v, want overrides only", overrideQueryType["enum"])
	}
}

func TestOpenAPICodeRelationshipEnumsIncludeImplementationAndInstantiation(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}

	paths := mustMapField(t, spec, "paths")
	for _, path := range []string{"/api/v0/code/relationships", "/api/v0/code/relationships/story"} {
		route := mustMapField(t, paths, path)
		post := mustMapField(t, route, "post")
		content := mustMapField(t, mustMapField(t, post, "requestBody"), "content")
		jsonBody := mustMapField(t, content, "application/json")
		properties := mustMapField(t, mustMapField(t, jsonBody, "schema"), "properties")
		relationshipType := mustMapField(t, properties, "relationship_type")
		enum, ok := relationshipType["enum"].([]any)
		if !ok {
			t.Fatalf("%s relationship_type enum type = %T, want []any", path, relationshipType["enum"])
		}
		for _, want := range []string{"IMPLEMENTS", "INSTANTIATES"} {
			if !containsValue(enum, want) {
				t.Fatalf("%s relationship_type enum = %#v, want %s", path, enum, want)
			}
		}
	}
}
