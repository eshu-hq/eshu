// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPISpecStatusPathsMatchCurrentContract(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := testutil.MustMapField(t, spec, "paths")
	if _, ok := paths["/api/v0/index-status"]; !ok {
		t.Fatal("OpenAPI paths missing /api/v0/index-status")
	}
	readinessPath := testutil.MustMapField(t, paths, "/api/v0/status/hosted-readiness")
	readinessGet := testutil.MustMapField(t, readinessPath, "get")
	readinessResponses := testutil.MustMapField(t, readinessGet, "responses")
	readinessOK := testutil.MustMapField(t, readinessResponses, "200")
	readinessContent := testutil.MustMapField(t, readinessOK, "content")
	readinessJSON := testutil.MustMapField(t, readinessContent, "application/json")
	readinessSchema := testutil.MustMapField(t, readinessJSON, "schema")
	readinessProperties := testutil.MustMapField(t, readinessSchema, "properties")
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
	collectorReadinessPath := testutil.MustMapField(t, paths, "/api/v0/status/collector-readiness")
	collectorReadinessGet := testutil.MustMapField(t, collectorReadinessPath, "get")
	collectorReadinessResponses := testutil.MustMapField(t, collectorReadinessGet, "responses")
	collectorReadinessOK := testutil.MustMapField(t, collectorReadinessResponses, "200")
	collectorReadinessContent := testutil.MustMapField(t, collectorReadinessOK, "content")
	collectorReadinessJSON := testutil.MustMapField(t, collectorReadinessContent, "application/json")
	collectorReadinessSchema := testutil.MustMapField(t, collectorReadinessJSON, "schema")
	collectorReadinessProperties := testutil.MustMapField(t, collectorReadinessSchema, "properties")
	readinessItems := testutil.MustMapField(t, collectorReadinessProperties, "readiness")
	readinessItemSchema := testutil.MustMapField(t, readinessItems, "items")
	readinessItemProperties := testutil.MustMapField(t, readinessItemSchema, "properties")
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

	operatorPath := testutil.MustMapField(t, paths, "/api/v0/status/operator-control-plane")
	operatorGet := testutil.MustMapField(t, operatorPath, "get")
	operatorResponses := testutil.MustMapField(t, operatorGet, "responses")
	operatorOK := testutil.MustMapField(t, operatorResponses, "200")
	operatorContent := testutil.MustMapField(t, operatorOK, "content")
	operatorJSON := testutil.MustMapField(t, operatorContent, "application/json")
	operatorSchema := testutil.MustMapField(t, operatorJSON, "schema")
	operatorProperties := testutil.MustMapField(t, operatorSchema, "properties")
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

	freshnessPath := testutil.MustMapField(t, paths, "/api/v0/status/freshness-causality")
	freshnessGet := testutil.MustMapField(t, freshnessPath, "get")
	freshnessResponses := testutil.MustMapField(t, freshnessGet, "responses")
	freshnessOK := testutil.MustMapField(t, freshnessResponses, "200")
	freshnessContent := testutil.MustMapField(t, freshnessOK, "content")
	freshnessJSON := testutil.MustMapField(t, freshnessContent, "application/json")
	freshnessSchema := testutil.MustMapField(t, freshnessJSON, "schema")
	freshnessProperties := testutil.MustMapField(t, freshnessSchema, "properties")
	for _, want := range []string{"state", "causes", "generations", "pending_projection", "recent_transitions", "scoped"} {
		if _, ok := freshnessProperties[want]; !ok {
			t.Fatalf("/api/v0/status/freshness-causality response schema missing %q", want)
		}
	}

	semanticPath := testutil.MustMapField(t, paths, "/api/v0/status/semantic-extraction")
	semanticGet := testutil.MustMapField(t, semanticPath, "get")
	semanticResponses := testutil.MustMapField(t, semanticGet, "responses")
	semanticOK := testutil.MustMapField(t, semanticResponses, "200")
	semanticContent := testutil.MustMapField(t, semanticOK, "content")
	semanticJSON := testutil.MustMapField(t, semanticContent, "application/json")
	semanticSchema := testutil.MustMapField(t, semanticJSON, "schema")
	semanticProperties := testutil.MustMapField(t, semanticSchema, "properties")
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
	providerProfiles := testutil.MustMapField(t, semanticProperties, "provider_profiles")
	providerProfileItems := testutil.MustMapField(t, providerProfiles, "items")
	providerProfileProperties := testutil.MustMapField(t, providerProfileItems, "properties")
	if _, ok := providerProfileProperties["embedding_dimensions"]; !ok {
		t.Fatal("semantic-extraction provider profile schema missing embedding_dimensions")
	}
	sourceClasses := testutil.MustMapField(t, providerProfileProperties, "source_classes")
	sourceClassItems := testutil.MustMapField(t, sourceClasses, "items")
	sourceClassEnums := mustStringSliceField(t, sourceClassItems, "enum")
	if !containsString(sourceClassEnums, "search_documents") {
		t.Fatalf("semantic-extraction source_classes enum = %#v, want search_documents", sourceClassEnums)
	}
	answerNarrationPath := testutil.MustMapField(t, paths, "/api/v0/status/answer-narration")
	answerNarrationGet := testutil.MustMapField(t, answerNarrationPath, "get")
	answerNarrationResponses := testutil.MustMapField(t, answerNarrationGet, "responses")
	answerNarrationOK := testutil.MustMapField(t, answerNarrationResponses, "200")
	answerNarrationContent := testutil.MustMapField(t, answerNarrationOK, "content")
	answerNarrationJSON := testutil.MustMapField(t, answerNarrationContent, "application/json")
	answerNarrationSchema := testutil.MustMapField(t, answerNarrationJSON, "schema")
	answerNarrationProperties := testutil.MustMapField(t, answerNarrationSchema, "properties")
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
	governancePath := testutil.MustMapField(t, paths, "/api/v0/status/governance")
	governanceGet := testutil.MustMapField(t, governancePath, "get")
	governanceResponses := testutil.MustMapField(t, governanceGet, "responses")
	governanceOK := testutil.MustMapField(t, governanceResponses, "200")
	governanceContent := testutil.MustMapField(t, governanceOK, "content")
	governanceJSON := testutil.MustMapField(t, governanceContent, "application/json")
	governanceSchema := testutil.MustMapField(t, governanceJSON, "schema")
	governanceProperties := testutil.MustMapField(t, governanceSchema, "properties")
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
	collectorsPath := testutil.MustMapField(t, paths, "/api/v0/status/collectors")
	collectorsGet := testutil.MustMapField(t, collectorsPath, "get")
	collectorsResponses := testutil.MustMapField(t, collectorsGet, "responses")
	collectorsOK := testutil.MustMapField(t, collectorsResponses, "200")
	collectorsContent := testutil.MustMapField(t, collectorsOK, "content")
	collectorsJSON := testutil.MustMapField(t, collectorsContent, "application/json")
	collectorsSchema := testutil.MustMapField(t, collectorsJSON, "schema")
	collectorsProperties := testutil.MustMapField(t, collectorsSchema, "properties")
	for _, want := range []string{"version", "updated_at", "collectors", "count", "classification_basis"} {
		if _, ok := collectorsProperties[want]; !ok {
			t.Fatalf("/api/v0/status/collectors response schema missing %q", want)
		}
	}
	collectorsList := testutil.MustMapField(t, collectorsProperties, "collectors")
	collectorItems := testutil.MustMapField(t, collectorsList, "items")
	collectorItemProperties := testutil.MustMapField(t, collectorItems, "properties")
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
