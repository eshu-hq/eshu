package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecStatusPathsMatchCurrentContract(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	if _, ok := paths["/api/v0/index-status"]; !ok {
		t.Fatal("OpenAPI paths missing /api/v0/index-status")
	}
	readinessPath := mustMapField(t, paths, "/api/v0/status/hosted-readiness")
	readinessGet := mustMapField(t, readinessPath, "get")
	readinessResponses := mustMapField(t, readinessGet, "responses")
	readinessOK := mustMapField(t, readinessResponses, "200")
	readinessContent := mustMapField(t, readinessOK, "content")
	readinessJSON := mustMapField(t, readinessContent, "application/json")
	readinessSchema := mustMapField(t, readinessJSON, "schema")
	readinessProperties := mustMapField(t, readinessSchema, "properties")
	for _, want := range []string{
		"state",
		"ready",
		"summary",
		"failure_classes",
		"checks",
		"diagnostic_paths",
	} {
		if _, ok := readinessProperties[want]; !ok {
			t.Fatalf("/api/v0/status/hosted-readiness response schema missing %q", want)
		}
	}
	semanticPath := mustMapField(t, paths, "/api/v0/status/semantic-extraction")
	semanticGet := mustMapField(t, semanticPath, "get")
	semanticResponses := mustMapField(t, semanticGet, "responses")
	semanticOK := mustMapField(t, semanticResponses, "200")
	semanticContent := mustMapField(t, semanticOK, "content")
	semanticJSON := mustMapField(t, semanticContent, "application/json")
	semanticSchema := mustMapField(t, semanticJSON, "schema")
	semanticProperties := mustMapField(t, semanticSchema, "properties")
	for _, want := range []string{
		"state",
		"reason",
		"code_hints_enabled",
		"documentation_observations_enabled",
		"deterministic_paths_affected",
		"queue",
		"budget",
		"audit",
	} {
		if _, ok := semanticProperties[want]; !ok {
			t.Fatalf("/api/v0/status/semantic-extraction response schema missing %q", want)
		}
	}
	governancePath := mustMapField(t, paths, "/api/v0/status/governance")
	governanceGet := mustMapField(t, governancePath, "get")
	governanceResponses := mustMapField(t, governanceGet, "responses")
	governanceOK := mustMapField(t, governanceResponses, "200")
	governanceContent := mustMapField(t, governanceOK, "content")
	governanceJSON := mustMapField(t, governanceContent, "application/json")
	governanceSchema := mustMapField(t, governanceJSON, "schema")
	governanceProperties := mustMapField(t, governanceSchema, "properties")
	for _, want := range []string{
		"mode",
		"state",
		"source_kind",
		"policy_revision_hash",
		"readiness",
		"identity",
		"egress",
		"semantic",
		"extensions",
		"redaction",
		"retention",
		"audit",
		"aggregates",
		"reasons",
	} {
		if _, ok := governanceProperties[want]; !ok {
			t.Fatalf("/api/v0/status/governance response schema missing %q", want)
		}
	}
	if _, ok := paths["/api/v0/ingesters"]; !ok {
		t.Fatal("OpenAPI paths missing /api/v0/ingesters")
	}
	if _, ok := paths["/api/v0/ingesters/{ingester}"]; !ok {
		t.Fatal("OpenAPI paths missing /api/v0/ingesters/{ingester}")
	}
	collectorsPath := mustMapField(t, paths, "/api/v0/status/collectors")
	collectorsGet := mustMapField(t, collectorsPath, "get")
	collectorsResponses := mustMapField(t, collectorsGet, "responses")
	collectorsOK := mustMapField(t, collectorsResponses, "200")
	collectorsContent := mustMapField(t, collectorsOK, "content")
	collectorsJSON := mustMapField(t, collectorsContent, "application/json")
	collectorsSchema := mustMapField(t, collectorsJSON, "schema")
	collectorsProperties := mustMapField(t, collectorsSchema, "properties")
	for _, want := range []string{"version", "updated_at", "collectors", "count", "classification_basis"} {
		if _, ok := collectorsProperties[want]; !ok {
			t.Fatalf("/api/v0/status/collectors response schema missing %q", want)
		}
	}
	collectorsList := mustMapField(t, collectorsProperties, "collectors")
	collectorItems := mustMapField(t, collectorsList, "items")
	collectorItemProperties := mustMapField(t, collectorItems, "properties")
	if _, ok := collectorItemProperties["observation_count"]; !ok {
		t.Fatal("/api/v0/status/collectors collector item schema missing observation_count")
	}
	if _, ok := collectorItemProperties["source_systems"]; !ok {
		t.Fatal("/api/v0/status/collectors collector item schema missing source_systems")
	}
	if _, ok := paths["/api/v0/index-runs/{run_id}"]; ok {
		t.Fatal("OpenAPI paths unexpectedly advertise /api/v0/index-runs/{run_id}")
	}
	if _, ok := paths["/api/v0/index-runs/{run_id}/coverage"]; ok {
		t.Fatal("OpenAPI paths unexpectedly advertise /api/v0/index-runs/{run_id}/coverage")
	}
}
