package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPIRelationshipSchemaIncludesResolutionProvenance(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}

	components := mustMapField(t, spec, "components")
	schemas := mustMapField(t, components, "schemas")
	relationship := mustMapField(t, schemas, "Relationship")
	properties := mustMapField(t, relationship, "properties")
	for _, field := range []string{"confidence", "resolution_method", "reason"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("Relationship schema missing %q in %#v", field, properties)
		}
	}
}
