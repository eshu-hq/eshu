package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecServiceInvestigationExposesCoverageFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	investigationPath := mustMapField(t, paths, "/api/v0/investigations/services/{service_name}")
	investigationGet := mustMapField(t, investigationPath, "get")
	responses := mustMapField(t, investigationGet, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	properties := mustMapField(t, mustMapField(t, content, "schema"), "properties")

	for _, field := range []string{
		"repositories_considered",
		"repositories_with_evidence",
		"evidence_families_found",
		"coverage_summary",
		"investigation_findings",
		"recommended_next_calls",
	} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("investigation response schema missing %s", field)
		}
	}
}
