// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

type valueFlowWhyTrailGraph struct{}

func (valueFlowWhyTrailGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return []map[string]any{{
		"direction":           "outgoing",
		"type":                "TAINT_FLOWS_TO",
		"source_id":           "func-source",
		"source_name":         "handler",
		"target_id":           "func-sink",
		"target_name":         "query",
		"confidence":          0.6,
		"evidence_source":     "reducer/code-interproc",
		"why_trail_json":      `[{"role":"source","function_uid":"func-source"},{"role":"sink","function_uid":"func-sink"}]`,
		"why_trail_truncated": true,
	}}, nil
}

func (valueFlowWhyTrailGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func TestCodeRelationshipValueFlowWhyTrailHTTPMCPParity(t *testing.T) {
	t.Parallel()

	handler := queryHandlerWithValueFlowWhyTrail()
	body := map[string]any{
		"entity_id":         "func-source",
		"relationship_type": "TAINT_FLOWS_TO",
		"direction":         "outgoing",
		"limit":             5,
	}
	httpEnv := httpEnvelope(t, handler, http.MethodPost, "/api/v0/code/relationships/story", body)
	mcpEnv, _ := mcpEnvelope(t, handler, "get_code_relationship_story", body)
	requireParity(t, "http", "mcp", extractComparable(t, httpEnv), extractComparable(t, mcpEnv))
	requireValueFlowWhyTrail(t, "http", httpEnv)
	requireValueFlowWhyTrail(t, "mcp", mcpEnv)
}

func TestCodeRelationshipValueFlowToolSchemasAdvertiseTaintFlowsTo(t *testing.T) {
	t.Parallel()

	storySchema := codeRelationshipStoryTool().InputSchema.(map[string]any)
	storyProperties := storySchema["properties"].(map[string]any)
	relationshipType := storyProperties["relationship_type"].(map[string]any)
	if !schemaEnumContains(relationshipType["enum"].([]string), "TAINT_FLOWS_TO") {
		t.Fatalf("story relationship_type enum missing TAINT_FLOWS_TO: %#v", relationshipType["enum"])
	}
	storyTypes := storyProperties["relationship_types"].(map[string]any)
	storyItems := storyTypes["items"].(map[string]any)
	if !schemaEnumContains(storyItems["enum"].([]string), "TAINT_FLOWS_TO") {
		t.Fatalf("story relationship_types enum missing TAINT_FLOWS_TO: %#v", storyItems["enum"])
	}

	analyzeSchema := analyzeCodeRelationshipsSchema()
	analyzeProperties := analyzeSchema["properties"].(map[string]any)
	analyzeTypes := analyzeProperties["relationship_types"].(map[string]any)
	analyzeItems := analyzeTypes["items"].(map[string]any)
	if !schemaEnumContains(analyzeItems["enum"].([]string), "TAINT_FLOWS_TO") {
		t.Fatalf("analyze relationship_types enum missing TAINT_FLOWS_TO: %#v", analyzeItems["enum"])
	}
}

func queryHandlerWithValueFlowWhyTrail() http.Handler {
	handler := &query.CodeHandler{Neo4j: valueFlowWhyTrailGraph{}}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

func requireValueFlowWhyTrail(t *testing.T, surface string, env *query.ResponseEnvelope) {
	t.Helper()

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("%s data type = %T, want map[string]any", surface, env.Data)
	}
	relationships, ok := data["relationships"].([]any)
	if !ok || len(relationships) != 1 {
		t.Fatalf("%s relationships = %#v, want one row", surface, data["relationships"])
	}
	row := relationships[0].(map[string]any)
	provenance := row["provenance"].(map[string]any)
	trail, ok := provenance["why_trail"].([]any)
	if !ok || len(trail) != 2 {
		t.Fatalf("%s why_trail = %#v, want two steps", surface, provenance["why_trail"])
	}
	if provenance["truth_state"] != "derived" || provenance["source_family"] != "value_flow_edge" {
		t.Fatalf("%s provenance = %#v, want derived value_flow_edge", surface, provenance)
	}
}

func schemaEnumContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
