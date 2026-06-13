package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPIRelationshipDocumentsSourceMetadata(t *testing.T) {
	var spec map[string]interface{}
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	relationship := mustMapField(t, mustMapField(t, mustMapField(t, spec, "components"), "schemas"), "Relationship")
	properties := mustMapField(t, relationship, "properties")
	for _, field := range []string{
		"source_repo_id",
		"source_repo_name",
		"source_file_path",
		"source_language",
		"source_type",
		"source_start_line",
		"source_end_line",
		"target_repo_id",
		"target_repo_name",
		"target_file_path",
		"target_language",
		"target_type",
		"target_start_line",
		"target_end_line",
	} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("Relationship schema missing %s", field)
		}
	}
}
