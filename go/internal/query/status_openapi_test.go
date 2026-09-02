// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecStatusPathsMatchCurrentContract(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	if _, ok := paths["/api/v0/index-status"]; !ok {
		t.Fatal("OpenAPI paths missing /api/v0/index-status")
	}
	readinessPath := querytestutil.MustMapField(t, paths, "/api/v0/status/hosted-readiness")
	readinessGet := querytestutil.MustMapField(t, readinessPath, "get")
	readinessResponses := querytestutil.MustMapField(t, readinessGet, "responses")
	readinessOK := querytestutil.MustMapField(t, readinessResponses, "200")
	readinessContent := querytestutil.MustMapField(t, readinessOK, "content")
	readinessJSON := querytestutil.MustMapField(t, readinessContent, "application/json")
	readinessSchema := querytestutil.MustMapField(t, readinessJSON, "schema")
	readinessProperties := querytestutil.MustMapField(t, readinessSchema, "properties")
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
	collectorReadinessPath := querytestutil.MustMapField(t, paths, "/api/v0/status/collector-readiness")
	collectorReadinessGet := querytestutil.MustMapField(t, collectorReadinessPath, "get")
	collectorReadinessResponses := querytestutil.MustMapField(t, collectorReadinessGet, "responses")
	collectorReadinessOK := querytestutil.MustMapField(t, collectorReadinessResponses, "200")
	collectorReadinessContent := querytestutil.MustMapField(t, collectorReadinessOK, "content")
	collectorReadinessJSON := querytestutil.MustMapField(t, collectorReadinessContent, "application/json")
	collectorReadinessSchema := querytestutil.MustMapField(t, collectorReadinessJSON, "schema")
	collectorReadinessProperties := querytestutil.MustMapField(t, collectorReadinessSchema, "properties")
	readinessItems := querytestutil.MustMapField(t, collectorReadinessProperties, "readiness")
	readinessItemSchema := querytestutil.MustMapField(t, readinessItems, "items")
	readinessItemProperties := querytestutil.MustMapField(t, readinessItemSchema, "properties")
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

	operatorPath := querytestutil.MustMapField(t, paths, "/api/v0/status/operator-control-plane")
	operatorGet := querytestutil.MustMapField(t, operatorPath, "get")
	operatorResponses := querytestutil.MustMapField(t, operatorGet, "responses")
	operatorOK := querytestutil.MustMapField(t, operatorResponses, "200")
	operatorContent := querytestutil.MustMapField(t, operatorOK, "content")
	operatorJSON := querytestutil.MustMapField(t, operatorContent, "application/json")
	operatorSchema := querytestutil.MustMapField(t, operatorJSON, "schema")
	operatorProperties := querytestutil.MustMapField(t, operatorSchema, "properties")
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

	freshnessPath := querytestutil.MustMapField(t, paths, "/api/v0/status/freshness-causality")
	freshnessGet := querytestutil.MustMapField(t, freshnessPath, "get")
	freshnessResponses := querytestutil.MustMapField(t, freshnessGet, "responses")
	freshnessOK := querytestutil.MustMapField(t, freshnessResponses, "200")
	freshnessContent := querytestutil.MustMapField(t, freshnessOK, "content")
	freshnessJSON := querytestutil.MustMapField(t, freshnessContent, "application/json")
	freshnessSchema := querytestutil.MustMapField(t, freshnessJSON, "schema")
	freshnessProperties := querytestutil.MustMapField(t, freshnessSchema, "properties")
	for _, want := range []string{"state", "causes", "generations", "pending_projection", "recent_transitions", "scoped"} {
		if _, ok := freshnessProperties[want]; !ok {
			t.Fatalf("/api/v0/status/freshness-causality response schema missing %q", want)
		}
	}

	semanticPath := querytestutil.MustMapField(t, paths, "/api/v0/status/semantic-extraction")
	semanticGet := querytestutil.MustMapField(t, semanticPath, "get")
	semanticResponses := querytestutil.MustMapField(t, semanticGet, "responses")
	semanticOK := querytestutil.MustMapField(t, semanticResponses, "200")
	semanticContent := querytestutil.MustMapField(t, semanticOK, "content")
	semanticJSON := querytestutil.MustMapField(t, semanticContent, "application/json")
	semanticSchema := querytestutil.MustMapField(t, semanticJSON, "schema")
	semanticProperties := querytestutil.MustMapField(t, semanticSchema, "properties")
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
	providerProfiles := querytestutil.MustMapField(t, semanticProperties, "provider_profiles")
	providerProfileItems := querytestutil.MustMapField(t, providerProfiles, "items")
	providerProfileProperties := querytestutil.MustMapField(t, providerProfileItems, "properties")
	if _, ok := providerProfileProperties["embedding_dimensions"]; !ok {
		t.Fatal("semantic-extraction provider profile schema missing embedding_dimensions")
	}
	sourceClasses := querytestutil.MustMapField(t, providerProfileProperties, "source_classes")
	sourceClassItems := querytestutil.MustMapField(t, sourceClasses, "items")
	sourceClassEnums := mustStringSliceField(t, sourceClassItems, "enum")
	if !containsString(sourceClassEnums, "search_documents") {
		t.Fatalf("semantic-extraction source_classes enum = %#v, want search_documents", sourceClassEnums)
	}
	answerNarrationPath := querytestutil.MustMapField(t, paths, "/api/v0/status/answer-narration")
	answerNarrationGet := querytestutil.MustMapField(t, answerNarrationPath, "get")
	answerNarrationResponses := querytestutil.MustMapField(t, answerNarrationGet, "responses")
	answerNarrationOK := querytestutil.MustMapField(t, answerNarrationResponses, "200")
	answerNarrationContent := querytestutil.MustMapField(t, answerNarrationOK, "content")
	answerNarrationJSON := querytestutil.MustMapField(t, answerNarrationContent, "application/json")
	answerNarrationSchema := querytestutil.MustMapField(t, answerNarrationJSON, "schema")
	answerNarrationProperties := querytestutil.MustMapField(t, answerNarrationSchema, "properties")
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
	governancePath := querytestutil.MustMapField(t, paths, "/api/v0/status/governance")
	governanceGet := querytestutil.MustMapField(t, governancePath, "get")
	governanceResponses := querytestutil.MustMapField(t, governanceGet, "responses")
	governanceOK := querytestutil.MustMapField(t, governanceResponses, "200")
	governanceContent := querytestutil.MustMapField(t, governanceOK, "content")
	governanceJSON := querytestutil.MustMapField(t, governanceContent, "application/json")
	governanceSchema := querytestutil.MustMapField(t, governanceJSON, "schema")
	governanceProperties := querytestutil.MustMapField(t, governanceSchema, "properties")
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
	collectorsPath := querytestutil.MustMapField(t, paths, "/api/v0/status/collectors")
	collectorsGet := querytestutil.MustMapField(t, collectorsPath, "get")
	collectorsResponses := querytestutil.MustMapField(t, collectorsGet, "responses")
	collectorsOK := querytestutil.MustMapField(t, collectorsResponses, "200")
	collectorsContent := querytestutil.MustMapField(t, collectorsOK, "content")
	collectorsJSON := querytestutil.MustMapField(t, collectorsContent, "application/json")
	collectorsSchema := querytestutil.MustMapField(t, collectorsJSON, "schema")
	collectorsProperties := querytestutil.MustMapField(t, collectorsSchema, "properties")
	for _, want := range []string{"version", "updated_at", "collectors", "count", "classification_basis"} {
		if _, ok := collectorsProperties[want]; !ok {
			t.Fatalf("/api/v0/status/collectors response schema missing %q", want)
		}
	}
	collectorsList := querytestutil.MustMapField(t, collectorsProperties, "collectors")
	collectorItems := querytestutil.MustMapField(t, collectorsList, "items")
	collectorItemProperties := querytestutil.MustMapField(t, collectorItems, "properties")
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
