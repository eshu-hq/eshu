package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPIRepositoryStatsDocumentsTimeoutMetadata(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	statsPath := mustMapField(t, paths, "/api/v0/repositories/{repo_id}/stats")
	statsGet := mustMapField(t, statsPath, "get")
	responses := mustMapField(t, statsGet, "responses")
	if _, ok := responses["504"]; !ok {
		t.Fatal("stats responses missing 504 timeout contract")
	}

	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	properties := mustMapField(t, mustMapField(t, content, "schema"), "properties")
	coverage := mustMapField(t, properties, "coverage")
	coverageProperties := mustMapField(t, coverage, "properties")
	for _, field := range []string{"partial_results", "truncated", "timeout", "timeout_budget", "missing_evidence"} {
		if _, ok := coverageProperties[field]; !ok {
			t.Fatalf("coverage schema missing %s", field)
		}
	}
	for _, field := range []string{"result_limits", "partial_reasons"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("stats schema missing additive %s block", field)
		}
	}
}

func TestOpenAPIRepositoryDocumentsGroupEvidenceFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	components := mustMapField(t, spec, "components")
	schemas := mustMapField(t, components, "schemas")
	repository := mustMapField(t, schemas, "Repository")
	properties := mustMapField(t, repository, "properties")
	for _, field := range []string{"group_key", "group_source", "group_truth", "group_kind", "group_reason"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("Repository schema missing %s", field)
		}
	}
}
