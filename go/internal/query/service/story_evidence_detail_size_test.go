// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/answer"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

const (
	// storyMCPResponseBudget is the MCP tool-response byte budget
	// (defaultToolResponseByteBudget in go/internal/mcp). The budget is not
	// raised for #8798; the test only reads it.
	storyMCPResponseBudget = 262144
	// storyMCPWrapperBytes allows for the JSON-RPC and content wrappers around
	// the two copies, which the model of mcpResultBytes leaves out.
	storyMCPWrapperBytes = 512
)

// mcpResultBytes models what the MCP dispatch serializes for a story: the
// structured copy of the {data, truth, error} envelope (data carries the
// answer_packet the HTTP layer attaches) plus the text copy, which is that
// envelope as a JSON string and so pays for every escaped quote. This is the
// dual result shape of #8796; a single-copy shape (#8800) would send one of
// the two.
func mcpResultBytes(t *testing.T, data map[string]any, omissions []querycontract.TruthOmission) int {
	t.Helper()
	truth := &querycontract.TruthEnvelope{Omissions: omissions}
	structured, err := json.Marshal(map[string]any{
		"data":  answer.ServiceStoryAnswerData("checkout-service", data, truth),
		"truth": truth,
		"error": nil,
	})
	if err != nil {
		t.Fatalf("json.Marshal(envelope) error = %v", err)
	}
	text, err := json.Marshal(string(structured))
	if err != nil {
		t.Fatalf("json.Marshal(text copy) error = %v", err)
	}
	return len(structured) + len(text)
}

// marshalStoryText returns the size of the escaped text copy that the dual
// result shape adds to the structured copy, so dual minus this is one copy.
func marshalStoryText(t *testing.T, data map[string]any, omissions []querycontract.TruthOmission) int {
	t.Helper()
	truth := &querycontract.TruthEnvelope{Omissions: omissions}
	structured, err := json.Marshal(map[string]any{
		"data":  answer.ServiceStoryAnswerData("checkout-service", data, truth),
		"truth": truth,
		"error": nil,
	})
	if err != nil {
		t.Fatalf("json.Marshal(envelope) error = %v", err)
	}
	text, err := json.Marshal(string(structured))
	if err != nil {
		t.Fatalf("json.Marshal(text copy) error = %v", err)
	}
	return len(text)
}

func marshalStory(t *testing.T, data map[string]any) int {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal(story) error = %v", err)
	}
	return len(raw)
}

// TestOversizedServiceStoryHandlesFitMCPBudget pins the #8798 failure: the
// story of a large service serializes past the whole MCP response budget with
// no bound, and the handles view brings it under half the budget so the doubled
// MCP copy fits.
func TestOversizedServiceStoryHandlesFitMCPBudget(t *testing.T) {
	t.Parallel()

	full := BuildServiceStoryResponse("checkout-service", testutil.OversizedServiceDossierContext())
	fullBytes := marshalStory(t, full)
	if fullBytes <= storyMCPResponseBudget {
		t.Fatalf("fixture story = %d bytes, want past the %d-byte budget so the test proves the bound", fullBytes, storyMCPResponseBudget)
	}

	handles := BuildServiceStoryResponse("checkout-service", testutil.OversizedServiceDossierContext())
	omissions := querycontract.ApplyStoryEvidenceDetail(handles, querycontract.ContextEvidenceDetailHandles)
	handlesBytes := marshalStory(t, handles)
	fullMCP := mcpResultBytes(t, full, nil)
	handlesMCP := mcpResultBytes(t, handles, omissions)
	t.Logf("story bytes: full=%d handles=%d (%.1f%% of full); MCP result bytes: full=%d handles=%d, budget=%d",
		fullBytes, handlesBytes, 100*float64(handlesBytes)/float64(fullBytes), fullMCP, handlesMCP, storyMCPResponseBudget)
	logStoryKeyBytes(t, "full", full)
	logStoryKeyBytes(t, "handles", handles)
	if fullMCP <= storyMCPResponseBudget {
		t.Fatalf("full MCP result = %d bytes, want past the %d-byte budget so the test proves the bound", fullMCP, storyMCPResponseBudget)
	}
	if handlesMCP+storyMCPWrapperBytes > storyMCPResponseBudget {
		t.Fatalf("handles MCP result = %d bytes plus %d wrapper, want <= the %d-byte budget",
			handlesMCP, storyMCPWrapperBytes, storyMCPResponseBudget)
	}
	if len(omissions) == 0 {
		t.Fatal("handles view reduced the story but reported no truth omissions")
	}
}

// TestCandidateHeavyServiceStoryHandlesMCPSize measures the MCP result for a
// service with no attached cloud resources and a full list of uncorrelated
// candidates, the reachable candidate-heavy story (the enrichment loads
// candidates only when no cloud resource attached). It gates the single-copy
// size, which is what a single-copy result shape (#8800) sends, and logs the
// dual size, which is past the budget until that shape ships: dispatch then
// falls back to the resource copy. The log line is the number to watch when a
// family grows.
func TestCandidateHeavyServiceStoryHandlesMCPSize(t *testing.T) {
	t.Parallel()

	handles := BuildServiceStoryResponse("checkout-service", testutil.OversizedServiceDossierCandidatesOnly())
	omissions := querycontract.ApplyStoryEvidenceDetail(handles, querycontract.ContextEvidenceDetailHandles)
	dual := mcpResultBytes(t, handles, omissions)
	textCopy := marshalStoryText(t, handles, omissions)
	structuredCopy := dual - textCopy
	t.Logf("candidate-heavy story: handles data=%d bytes, MCP result=%d bytes (dual shape), structured copy=%d bytes, escaped text copy=%d bytes, budget=%d",
		marshalStory(t, handles), dual, structuredCopy, textCopy, storyMCPResponseBudget)
	// The escaped text copy is the larger of the two, so it bounds either
	// choice a single-copy shape makes.
	single := textCopy
	if single+storyMCPWrapperBytes > storyMCPResponseBudget {
		t.Fatalf("candidate-heavy escaped single copy = %d bytes plus %d wrapper, want <= the %d-byte budget",
			single, storyMCPWrapperBytes, storyMCPResponseBudget)
	}
}

// logStoryKeyBytes logs the serialized size of each top-level story key,
// largest first, so a regression names the family that grew.
func logStoryKeyBytes(t *testing.T, label string, data map[string]any) {
	t.Helper()
	type keyBytes struct {
		key   string
		bytes int
	}
	sizes := make([]keyBytes, 0, len(data))
	for key, value := range data {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("json.Marshal(%s) error = %v", key, err)
		}
		sizes = append(sizes, keyBytes{key, len(raw)})
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i].bytes > sizes[j].bytes })
	for _, size := range sizes {
		t.Logf("%s %-30s %7d", label, size.key, size.bytes)
	}
}

// TestOversizedServiceStoryHandlesKeepTruthAndCounts proves the handles view
// changes only what ships: the truth-bearing and count-bearing fields are
// byte-identical to the full view, every projected list keeps its length, and
// every cut carries a marker and an omission with its total (a lower bound for
// investigation.repositories_with_evidence, which records no count past 50).
func TestOversizedServiceStoryHandlesKeepTruthAndCounts(t *testing.T) {
	t.Parallel()

	full := BuildServiceStoryResponse("checkout-service", testutil.OversizedServiceDossierContext())
	handles := BuildServiceStoryResponse("checkout-service", testutil.OversizedServiceDossierContext())
	omissions := querycontract.ApplyStoryEvidenceDetail(handles, querycontract.ContextEvidenceDetailHandles)

	for _, key := range []string{
		"service_name", "story", "story_sections", "answer_metadata", "result_limits",
		"service_identity", "deployment_lanes", "raw_context_limits", "documentation_overview",
	} {
		want, _ := json.Marshal(full[key])
		got, _ := json.Marshal(handles[key])
		if string(got) != string(want) {
			t.Fatalf("%s changed under handles:\n got  %s\n want %s", key, got, want)
		}
	}

	fullOverview := querycontract.MapValue(full, "deployment_overview")
	handlesOverview := querycontract.MapValue(handles, "deployment_overview")
	for _, key := range []string{"instance_count", "environment_count", "hostname_count", "dependent_count", "deployment_evidence_artifact_count"} {
		if querycontract.IntVal(handlesOverview, key) != querycontract.IntVal(fullOverview, key) {
			t.Fatalf("deployment_overview.%s = %v under handles, want %v", key, handlesOverview[key], fullOverview[key])
		}
	}

	for _, key := range []string{"upstream_dependencies", "dependents", "consumer_repositories", "provisioning_source_chains", "cloud_resources", "hostnames", "entrypoints"} {
		if got, want := len(querycontract.MapSliceValue(handles, key)), len(querycontract.MapSliceValue(full, key)); got != want {
			t.Fatalf("len(%s) = %d under handles, want %d (a list keeps its length)", key, got, want)
		}
	}

	bySection := map[string]querycontract.TruthOmission{}
	for _, omission := range omissions {
		bySection[omission.Section] = omission
	}
	fullShipped := map[string]int{}
	for _, segment := range querycontract.MapSliceValue(querycontract.MapValue(full, "code_to_runtime_trace"), "segments") {
		fullShipped[querycontract.StringVal(segment, "name")] = len(querycontract.MapSliceValue(segment, "evidence"))
	}
	cutSegments := 0
	for _, segment := range querycontract.MapSliceValue(querycontract.MapValue(handles, "code_to_runtime_trace"), "segments") {
		name := querycontract.StringVal(segment, "name")
		count := querycontract.IntVal(segment, "evidence_count")
		shipped := len(querycontract.MapSliceValue(segment, "evidence"))
		if shipped < fullShipped[name] {
			cutSegments++
			if !querycontract.BoolVal(segment, "truncated") ||
				querycontract.IntVal(segment, "evidence_shipped") != shipped ||
				querycontract.IntVal(segment, "evidence_omitted") != count-shipped {
				t.Fatalf("segment %s ships %d of %d evidence rows without truncated, evidence_shipped, and a matching evidence_omitted", name, shipped, count)
			}
		}
		if shipped > 0 {
			if got := bySection["code_to_runtime_trace."+name+".evidence"].Total; got != count {
				t.Fatalf("omission for segment %s total = %d, want %d", name, got, count)
			}
		}
	}
	if cutSegments == 0 {
		t.Fatal("no trace segment was cut; the fixture no longer exercises the preview limit")
	}

	downstream := querycontract.MapValue(handles, "downstream_consumers")
	if _, has := downstream["graph_dependents"]; has {
		t.Fatal("downstream_consumers.graph_dependents ships twice; want the rows at dependents only")
	}
	if got := querycontract.StringVal(downstream, "graph_dependents_shipped_at"); got != "dependents" {
		t.Fatalf("graph_dependents_shipped_at = %q, want dependents", got)
	}
	if got, want := querycontract.IntVal(downstream, "graph_dependent_count"), querycontract.IntVal(querycontract.MapValue(full, "downstream_consumers"), "graph_dependent_count"); got != want {
		t.Fatalf("graph_dependent_count = %d, want %d (counts stay)", got, want)
	}

	graph := querycontract.MapValue(handles, "evidence_graph")
	fullGraph := querycontract.MapValue(full, "evidence_graph")
	wantNodes := len(querycontract.MapSliceValue(fullGraph, "nodes"))
	if got := querycontract.IntVal(graph, "node_count"); got != wantNodes {
		t.Fatalf("evidence_graph.node_count = %d, want %d", got, wantNodes)
	}
	shippedNodes := len(querycontract.MapSliceValue(graph, "nodes"))
	if !querycontract.BoolVal(graph, "nodes_truncated") || querycontract.IntVal(graph, "nodes_omitted") != wantNodes-shippedNodes {
		t.Fatalf("evidence_graph node cut has no truncated marker or omitted count: %#v", graph)
	}
	if got, want := querycontract.IntVal(graph, "edge_count"), querycontract.IntVal(fullGraph, "edge_count"); got != want {
		t.Fatalf("evidence_graph.edge_count = %d, want %d", got, want)
	}
	if bySection["evidence_graph.nodes"].Total != wantNodes {
		t.Fatalf("evidence_graph.nodes omission total = %d, want %d", bySection["evidence_graph.nodes"].Total, wantNodes)
	}
	if drilldown := querycontract.MapValue(handles, "evidence_detail_drilldown"); querycontract.StringVal(drilldown, "artifact_tool") != "get_relationship_evidence" {
		t.Fatalf("evidence_detail_drilldown = %#v, want it to name get_relationship_evidence", drilldown)
	}
}

// TestOversizedServiceStoryFullIsUnshaped proves full keeps today's body: the
// shaper only records the level.
func TestOversizedServiceStoryFullIsUnshaped(t *testing.T) {
	t.Parallel()

	built := BuildServiceStoryResponse("checkout-service", testutil.OversizedServiceDossierContext())
	want, _ := json.Marshal(built)
	for _, detail := range []string{"", querycontract.ContextEvidenceDetailFull} {
		shaped := BuildServiceStoryResponse("checkout-service", testutil.OversizedServiceDossierContext())
		if omissions := querycontract.ApplyStoryEvidenceDetail(shaped, detail); len(omissions) != 0 {
			t.Fatalf("detail %q omissions = %#v, want none", detail, omissions)
		}
		if got := querycontract.StringVal(shaped, "evidence_detail"); got != querycontract.ContextEvidenceDetailFull {
			t.Fatalf("detail %q evidence_detail = %q, want full", detail, got)
		}
		delete(shaped, "evidence_detail")
		got, _ := json.Marshal(shaped)
		if string(got) != string(want) {
			t.Fatalf("detail %q changed the full body", detail)
		}
	}
}
