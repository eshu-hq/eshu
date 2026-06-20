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
	if _, ok := paths["/api/v0/collector-readiness"]; !ok {
		t.Fatal("OpenAPI paths missing /api/v0/collector-readiness alias")
	}
	collectorReadinessPath := mustMapField(t, paths, "/api/v0/status/collector-readiness")
	collectorReadinessGet := mustMapField(t, collectorReadinessPath, "get")
	collectorReadinessResponses := mustMapField(t, collectorReadinessGet, "responses")
	collectorReadinessOK := mustMapField(t, collectorReadinessResponses, "200")
	collectorReadinessContent := mustMapField(t, collectorReadinessOK, "content")
	collectorReadinessJSON := mustMapField(t, collectorReadinessContent, "application/json")
	collectorReadinessSchema := mustMapField(t, collectorReadinessJSON, "schema")
	collectorReadinessProperties := mustMapField(t, collectorReadinessSchema, "properties")
	readinessItems := mustMapField(t, collectorReadinessProperties, "readiness")
	readinessItemSchema := mustMapField(t, readinessItems, "items")
	readinessItemProperties := mustMapField(t, readinessItemSchema, "properties")
	for _, want := range []string{
		"collector_kind",
		"promotion_state",
		"reducer_readback",
		"recommended_next_action",
	} {
		if _, ok := readinessItemProperties[want]; !ok {
			t.Fatalf("/api/v0/status/collector-readiness item schema missing %q", want)
		}
	}

	operatorPath := mustMapField(t, paths, "/api/v0/status/operator-control-plane")
	operatorGet := mustMapField(t, operatorPath, "get")
	operatorResponses := mustMapField(t, operatorGet, "responses")
	operatorOK := mustMapField(t, operatorResponses, "200")
	operatorContent := mustMapField(t, operatorOK, "content")
	operatorJSON := mustMapField(t, operatorContent, "application/json")
	operatorSchema := mustMapField(t, operatorJSON, "schema")
	operatorProperties := mustMapField(t, operatorSchema, "properties")
	for _, want := range []string{
		"queue",
		"reducer_domains",
		"collector_families",
		"dead_letters",
		"retry_policies",
		"scoped",
	} {
		if _, ok := operatorProperties[want]; !ok {
			t.Fatalf("/api/v0/status/operator-control-plane response schema missing %q", want)
		}
	}

	freshnessPath := mustMapField(t, paths, "/api/v0/status/freshness-causality")
	freshnessGet := mustMapField(t, freshnessPath, "get")
	freshnessResponses := mustMapField(t, freshnessGet, "responses")
	freshnessOK := mustMapField(t, freshnessResponses, "200")
	freshnessContent := mustMapField(t, freshnessOK, "content")
	freshnessJSON := mustMapField(t, freshnessContent, "application/json")
	freshnessSchema := mustMapField(t, freshnessJSON, "schema")
	freshnessProperties := mustMapField(t, freshnessSchema, "properties")
	for _, want := range []string{"state", "causes", "generations", "pending_projection", "recent_transitions", "scoped"} {
		if _, ok := freshnessProperties[want]; !ok {
			t.Fatalf("/api/v0/status/freshness-causality response schema missing %q", want)
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
		"provider_profiles",
	} {
		if _, ok := semanticProperties[want]; !ok {
			t.Fatalf("/api/v0/status/semantic-extraction response schema missing %q", want)
		}
	}
	providerProfiles := mustMapField(t, semanticProperties, "provider_profiles")
	providerProfileItems := mustMapField(t, providerProfiles, "items")
	providerProfileProperties := mustMapField(t, providerProfileItems, "properties")
	if _, ok := providerProfileProperties["embedding_dimensions"]; !ok {
		t.Fatal("semantic-extraction provider profile schema missing embedding_dimensions")
	}
	sourceClasses := mustMapField(t, providerProfileProperties, "source_classes")
	sourceClassItems := mustMapField(t, sourceClasses, "items")
	sourceClassEnums := mustStringSliceField(t, sourceClassItems, "enum")
	if !containsString(sourceClassEnums, "search_documents") {
		t.Fatalf("semantic-extraction source_classes enum = %#v, want search_documents", sourceClassEnums)
	}
	answerNarrationPath := mustMapField(t, paths, "/api/v0/status/answer-narration")
	answerNarrationGet := mustMapField(t, answerNarrationPath, "get")
	answerNarrationResponses := mustMapField(t, answerNarrationGet, "responses")
	answerNarrationOK := mustMapField(t, answerNarrationResponses, "200")
	answerNarrationContent := mustMapField(t, answerNarrationOK, "content")
	answerNarrationJSON := mustMapField(t, answerNarrationContent, "application/json")
	answerNarrationSchema := mustMapField(t, answerNarrationJSON, "schema")
	answerNarrationProperties := mustMapField(t, answerNarrationSchema, "properties")
	for _, want := range []string{
		"state",
		"reason",
		"deterministic_fallback_available",
		"provider_traffic_enabled",
		"canonical_truth_affected",
		"retention_posture",
		"supported_reasons",
		"validator_reason_codes",
	} {
		if _, ok := answerNarrationProperties[want]; !ok {
			t.Fatalf("/api/v0/status/answer-narration response schema missing %q", want)
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
