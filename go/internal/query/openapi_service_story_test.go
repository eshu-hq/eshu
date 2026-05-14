package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecServiceStoryExposesDossierFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	serviceStoryPath := mustMapField(t, paths, "/api/v0/services/{service_name}/story")
	serviceStoryGet := mustMapField(t, serviceStoryPath, "get")
	serviceStoryResponses := mustMapField(t, serviceStoryGet, "responses")
	serviceStoryOK := mustMapField(t, serviceStoryResponses, "200")
	serviceStoryContent := mustMapField(t, mustMapField(t, serviceStoryOK, "content"), "application/json")
	serviceStorySchema := mustMapField(t, mustMapField(t, serviceStoryContent, "schema"), "properties")

	for _, field := range []string{
		"service_identity",
		"api_surface",
		"deployment_lanes",
		"upstream_dependencies",
		"downstream_consumers",
		"evidence_graph",
		"result_limits",
		"investigation",
	} {
		if _, ok := serviceStorySchema[field]; !ok {
			t.Fatalf("services/{service_name}/story response schema missing %s", field)
		}
	}
}
