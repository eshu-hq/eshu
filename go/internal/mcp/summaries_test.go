// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func freshTruth(level query.TruthLevel, state query.FreshnessState) *query.TruthEnvelope {
	return &query.TruthEnvelope{
		Level:     level,
		Freshness: query.TruthFreshness{State: state},
	}
}

func TestSummarizeToolTextServiceStory(t *testing.T) {
	env := &query.ResponseEnvelope{
		Truth: freshTruth(query.TruthLevelExact, query.FreshnessFresh),
		Data: map[string]any{
			"service_identity": map[string]any{
				"service_name": "payments-api",
				"limitations":  []any{"identity_only materialization", "missing deployment evidence"},
			},
			"api_surface": map[string]any{
				"endpoint_count": float64(12),
				"truncated":      true,
			},
			"result_limits": map[string]any{
				"upstream_count":   float64(3),
				"downstream_count": float64(7),
			},
		},
	}

	got := summarizeToolText("get_service_story", env)

	for _, want := range []string{"payments-api", "exact", "fresh", "12", "truncated", "deps 3", "consumers 7"} {
		if !strings.Contains(got, want) {
			t.Fatalf("service story summary %q missing %q", got, want)
		}
	}
	if len(got) > maxSummaryLength {
		t.Fatalf("summary length %d exceeds cap %d", len(got), maxSummaryLength)
	}
}

func TestSummarizeToolTextServiceStoryDeterministic(t *testing.T) {
	env := &query.ResponseEnvelope{
		Truth: freshTruth(query.TruthLevelDerived, query.FreshnessStale),
		Data: map[string]any{
			"service_identity": map[string]any{"service_name": "orders"},
			"api_surface":      map[string]any{"endpoint_count": float64(1)},
			"result_limits":    map[string]any{"upstream_count": float64(0), "downstream_count": float64(0)},
		},
	}
	first := summarizeToolText("get_service_story", env)
	for i := 0; i < 5; i++ {
		if again := summarizeToolText("get_service_story", env); again != first {
			t.Fatalf("non-deterministic summary: %q != %q", again, first)
		}
	}
}

func TestSummarizeToolTextInvestigation(t *testing.T) {
	env := &query.ResponseEnvelope{
		Truth: freshTruth(query.TruthLevelDerived, query.FreshnessFresh),
		Data: map[string]any{
			"service_name": "checkout",
			"investigation_findings": []any{
				map[string]any{"family": "api_surface"},
				map[string]any{"family": "deployment_lanes"},
			},
			"coverage_summary": map[string]any{
				"state":     "partial",
				"truncated": true,
			},
			"recommended_next_calls": []any{
				map[string]any{"tool": "get_service_story"},
			},
		},
	}

	got := summarizeToolText("investigate_service", env)
	for _, want := range []string{"checkout", "2 finding", "partial", "next: get_service_story"} {
		if !strings.Contains(got, want) {
			t.Fatalf("investigation summary %q missing %q", got, want)
		}
	}
}

func TestSummarizeToolTextIncidentContext(t *testing.T) {
	env := &query.ResponseEnvelope{
		Truth: freshTruth(query.TruthLevelDerived, query.FreshnessFresh),
		Data: map[string]any{
			"incident": map[string]any{"title": "checkout latency spike"},
			"timeline": []any{map[string]any{"event_id": "e1"}},
			"related_changes": []any{
				map[string]any{"change_id": "c1"},
				map[string]any{"change_id": "c2"},
			},
			"missing_evidence":   []any{map[string]any{"slot": "image"}},
			"ambiguous_evidence": []any{map[string]any{"slot": "applied_routing"}},
			"truncated":          true,
		},
	}

	got := summarizeToolText("get_incident_context", env)
	for _, want := range []string{"checkout latency spike", "2 related change", "missing evidence 1", "ambiguous 1", "truncated"} {
		if !strings.Contains(got, want) {
			t.Fatalf("incident summary %q missing %q", got, want)
		}
	}
}

func TestSummarizeToolTextUsesAnswerMetadataWithoutMutatingData(t *testing.T) {
	env := &query.ResponseEnvelope{
		Truth: freshTruth(query.TruthLevelDerived, query.FreshnessFresh),
		Data: map[string]any{
			"service_identity": map[string]any{
				"service_name": "payments-api",
				"limitations":  []any{},
			},
			"api_surface":   map[string]any{"endpoint_count": float64(2), "truncated": false},
			"result_limits": map[string]any{"upstream_count": float64(1), "downstream_count": float64(0)},
			"answer_metadata": map[string]any{
				"schema_version":   "answer_metadata.v1",
				"truncated":        true,
				"missing_evidence": []any{map[string]any{"reason": "runtime evidence missing"}},
				"limitations":      []any{map[string]any{"reason": "metadata limitation"}},
				"coverage":         map[string]any{"query_shape": "service_story"},
			},
		},
	}
	got := summarizeToolText("get_service_story", env)

	for _, want := range []string{"truncated", "top limitation: metadata limitation"} {
		if !strings.Contains(got, want) {
			t.Fatalf("service story metadata summary %q missing %q", got, want)
		}
	}
	after, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("env.Data type = %T, want map", env.Data)
	}
	if _, ok := after["answer_metadata"]; !ok {
		t.Fatal("summarizeToolText removed answer_metadata from structured data")
	}
	if query.StringVal(after["service_identity"].(map[string]any), "service_name") != "payments-api" {
		t.Fatal("summarizeToolText mutated service_identity")
	}
}

func TestSummarizeIncidentContextUsesAnswerMetadataMissingEvidence(t *testing.T) {
	env := &query.ResponseEnvelope{
		Truth: freshTruth(query.TruthLevelDerived, query.FreshnessFresh),
		Data: map[string]any{
			"incident":           map[string]any{"title": "checkout latency spike"},
			"related_changes":    []any{},
			"missing_evidence":   []any{},
			"ambiguous_evidence": []any{},
			"answer_metadata": map[string]any{
				"schema_version": "answer_metadata.v1",
				"truncated":      true,
				"missing_evidence": []any{
					map[string]any{"slot": "image", "reason": "missing image evidence"},
					map[string]any{"slot": "commit", "reason": "missing commit evidence"},
				},
				"coverage": map[string]any{"query_shape": "incident_context_evidence_path"},
			},
		},
	}

	got := summarizeToolText("get_incident_context", env)

	for _, want := range []string{"missing evidence 2", "truncated"} {
		if !strings.Contains(got, want) {
			t.Fatalf("incident metadata summary %q missing %q", got, want)
		}
	}
}

func TestSummarizeToolTextCitationPacket(t *testing.T) {
	env := &query.ResponseEnvelope{
		Truth: freshTruth(query.TruthLevelDerived, query.FreshnessFresh),
		Data: map[string]any{
			"coverage": map[string]any{
				"resolved_count":     float64(4),
				"missing_count":      float64(2),
				"input_handle_count": float64(6),
				"truncated":          true,
			},
		},
	}

	got := summarizeToolText("build_evidence_citation_packet", env)
	for _, want := range []string{"coverage", "resolved 4", "requested 6", "missing 2", "truncated"} {
		if !strings.Contains(got, want) {
			t.Fatalf("citation summary %q missing %q", got, want)
		}
	}
}

func TestSummarizeToolTextErrorEnvelopeSurfacesCodeAndReason(t *testing.T) {
	env := &query.ResponseEnvelope{
		Error: &query.ErrorEnvelope{
			Code:    query.ErrorCodeIndexBuilding,
			Message: "index is still building for this scope",
		},
	}

	got := summarizeToolText("get_service_story", env)
	for _, want := range []string{"index_building", "index is still building"} {
		if !strings.Contains(got, want) {
			t.Fatalf("error summary %q missing %q", got, want)
		}
	}
}

func TestSummarizeToolTextBuildingFreshnessSurfacedForStory(t *testing.T) {
	env := &query.ResponseEnvelope{
		Truth: &query.TruthEnvelope{
			Level: query.TruthLevelDerived,
			Freshness: query.TruthFreshness{
				State:  query.FreshnessBuilding,
				Detail: "reducer projection in progress",
			},
		},
		Data: map[string]any{
			"service_identity": map[string]any{"service_name": "svc"},
			"api_surface":      map[string]any{"endpoint_count": float64(0)},
			"result_limits":    map[string]any{"upstream_count": float64(0), "downstream_count": float64(0)},
		},
	}

	got := summarizeToolText("get_service_story", env)
	if !strings.Contains(got, "building") {
		t.Fatalf("story summary %q should surface building freshness", got)
	}
}

func TestSummarizePlainToolTextIndexStatus(t *testing.T) {
	value := map[string]any{
		"status":           "degraded",
		"reasons":          []any{"queue backlog growing", "stale projections"},
		"repository_count": float64(9),
	}

	got := summarizePlainToolText("get_index_status", value)
	for _, want := range []string{"degraded", "queue backlog growing"} {
		if !strings.Contains(got, want) {
			t.Fatalf("index status summary %q missing %q", got, want)
		}
	}
}

func TestSummarizePlainToolTextHostedReadiness(t *testing.T) {
	value := map[string]any{
		"state":           "not_ready",
		"failure_classes": []any{"queue_not_drained", "graph_unavailable"},
	}

	got := summarizePlainToolText("get_hosted_readiness", value)
	for _, want := range []string{"not_ready", "queue_not_drained"} {
		if !strings.Contains(got, want) {
			t.Fatalf("hosted readiness summary %q missing %q", got, want)
		}
	}
}

func TestSummarizePlainToolTextIngesterStatus(t *testing.T) {
	value := map[string]any{
		"ingester": "repository",
		"health": map[string]any{
			"state":   "healthy",
			"reasons": []any{},
		},
		"queue": map[string]any{"outstanding": float64(0)},
	}

	got := summarizePlainToolText("get_ingester_status", value)
	for _, want := range []string{"repository", "healthy"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ingester status summary %q missing %q", got, want)
		}
	}
}

func TestSummarizeToolTextFallsBackForUnknownTool(t *testing.T) {
	env := &query.ResponseEnvelope{
		Truth: freshTruth(query.TruthLevelExact, query.FreshnessFresh),
		Data:  map[string]any{"count": float64(5)},
	}
	got := summarizeToolText("find_code", env)
	if !strings.Contains(got, "5") {
		t.Fatalf("fallback summary %q should include count", got)
	}
}

func TestSummaryBoundedAndTruncatesLimitations(t *testing.T) {
	longName := strings.Repeat("x", 4000)
	env := &query.ResponseEnvelope{
		Truth: freshTruth(query.TruthLevelExact, query.FreshnessFresh),
		Data: map[string]any{
			"service_identity": map[string]any{
				"service_name": longName,
				"limitations":  []any{strings.Repeat("limit-", 2000)},
			},
			"api_surface":   map[string]any{"endpoint_count": float64(1)},
			"result_limits": map[string]any{"upstream_count": float64(0), "downstream_count": float64(0)},
		},
	}
	got := summarizeToolText("get_service_story", env)
	if len(got) > maxSummaryLength {
		t.Fatalf("summary length %d exceeds cap %d", len(got), maxSummaryLength)
	}
}

// TestSummarizeToolTextMentionsFreshnessCause proves a proven freshness cause is
// surfaced in the convenience text while the structured envelope is untouched.
func TestSummarizeToolTextMentionsFreshnessCause(t *testing.T) {
	truth := &query.TruthEnvelope{
		Level:     query.TruthLevelDerived,
		Freshness: query.TruthFreshness{State: query.FreshnessStale},
	}
	query.WithFreshnessCause(truth, query.FreshnessCauseDeadLetteredDomain)
	env := &query.ResponseEnvelope{
		Truth: truth,
		Data:  map[string]any{"count": float64(2)},
	}

	got := summarizeToolText("unknown_tool", env)
	if !strings.Contains(got, "stale") {
		t.Fatalf("summary %q should surface stale freshness", got)
	}
	if !strings.Contains(got, string(query.FreshnessCauseDeadLetteredDomain)) {
		t.Fatalf("summary %q should mention the dead_lettered_domain cause", got)
	}

	// Structured content remains canonical: the summary must not mutate it.
	if env.Truth.Freshness.Cause != query.FreshnessCauseDeadLetteredDomain {
		t.Fatalf("summary mutated the structured cause: %q", env.Truth.Freshness.Cause)
	}
	if env.Truth.Freshness.NextCheck == nil {
		t.Fatalf("summary dropped the structured next check")
	}
}

// TestSummarizeToolTextFreshAnswerOmitsCause proves a fresh answer surfaces no
// cause note even if one were somehow attached, matching the not-fresh gate.
func TestSummarizeToolTextFreshAnswerOmitsCause(t *testing.T) {
	env := &query.ResponseEnvelope{
		Truth: freshTruth(query.TruthLevelExact, query.FreshnessFresh),
		Data:  map[string]any{"count": float64(1)},
	}
	got := summarizeToolText("unknown_tool", env)
	if strings.Contains(got, "cause:") {
		t.Fatalf("fresh summary %q should not carry a cause note", got)
	}
}
