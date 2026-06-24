// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestVisualizationPacketToolRegisteredAndRoutesToDeriveSurface(t *testing.T) {
	t.Parallel()

	found := false
	for _, tool := range ReadOnlyTools() {
		if tool.Name == "derive_visualization_packet" {
			found = true
			if !strings.Contains(tool.Description, "visualization packet") {
				t.Fatalf("tool description = %q, want visualization packet context", tool.Description)
			}
			break
		}
	}
	if !found {
		t.Fatal("ReadOnlyTools() missing derive_visualization_packet")
	}

	route, err := resolveRoute("derive_visualization_packet", map[string]any{
		"view": "service_story",
		"source_response": map[string]any{
			"service_identity": map[string]any{"service_id": "svc-1"},
		},
		"source_truth": map[string]any{"level": "exact"},
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.method != http.MethodPost || route.path != "/api/v0/visualizations/derive" {
		t.Fatalf("route = %s %s, want POST /api/v0/visualizations/derive", route.method, route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route body type = %T, want map[string]any", route.body)
	}
	if body["view"] != "service_story" {
		t.Fatalf("body view = %#v, want service_story", body["view"])
	}
	if _, ok := body["source_response"].(map[string]any); !ok {
		t.Fatalf("body source_response = %#v, want map", body["source_response"])
	}
	if _, ok := body["source_truth"].(map[string]any); !ok {
		t.Fatalf("body source_truth = %#v, want map", body["source_truth"])
	}
}

func TestVisualizationPacketMCPMatchesHTTPEnvelope(t *testing.T) {
	t.Parallel()

	handler := mountVisualizationHandlerForMCP()
	args := map[string]any{
		"view": "service_story",
		"source_response": map[string]any{
			"service_identity": map[string]any{
				"service_id":   "svc-1",
				"service_name": "payments",
				"repo_id":      "repo-payments",
			},
			"upstream_dependencies": []map[string]any{
				{
					"source":            "billing",
					"source_repo_id":    "repo-billing",
					"target_repo_id":    "repo-payments",
					"relationship_type": "DEPENDS_ON",
				},
			},
			"downstream_consumers": map[string]any{},
		},
		"source_truth": map[string]any{
			"level":      "exact",
			"basis":      "authoritative_graph",
			"capability": "platform_impact.service_story",
			"freshness":  map[string]any{"state": "fresh"},
		},
	}

	httpEnv := httpEnvelope(t, handler, http.MethodPost, "/api/v0/visualizations/derive", args)
	mcpEnv, summary := mcpEnvelope(t, handler, "derive_visualization_packet", args)
	if !equalJSON(httpEnv.Data, mcpEnv.Data) {
		t.Fatalf("HTTP/MCP data mismatch:\nHTTP=%#v\nMCP=%#v", httpEnv.Data, mcpEnv.Data)
	}
	if !equalJSON(httpEnv.Truth, mcpEnv.Truth) {
		t.Fatalf("HTTP/MCP truth mismatch:\nHTTP=%#v\nMCP=%#v", httpEnv.Truth, mcpEnv.Truth)
	}
	if strings.TrimSpace(summary) == "" {
		t.Fatal("summary empty, want convenience text")
	}
	if !strings.Contains(summary, "service_story") {
		t.Fatalf("summary = %q, want visualization view", summary)
	}
}

func TestVisualizationPacketMCPReturnsStructuredEnvelopeResource(t *testing.T) {
	t.Parallel()

	handler := mountVisualizationHandlerForMCP()
	server := NewServer(handler, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := &jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      9,
		Method:  "tools/call",
		Params: mustRawMessage(t, map[string]any{
			"name": "derive_visualization_packet",
			"arguments": map[string]any{
				"view":            "evidence_citation",
				"source_response": evidenceCitationSource(),
				"source_truth": map[string]any{
					"level":     "exact",
					"basis":     "content_index",
					"freshness": map[string]any{"state": "fresh"},
				},
			},
		}),
	}

	resp := server.handleMessage(t.Context(), req, "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("response = %+v, want success", resp)
	}
	result, ok := resp.Result.(mcpToolResult)
	if !ok {
		t.Fatalf("result type = %T, want mcpToolResult", resp.Result)
	}
	if result.StructuredContent == nil {
		t.Fatal("structured content nil, want canonical envelope")
	}
	if len(result.Content) != 2 {
		t.Fatalf("content len = %d, want text plus resource", len(result.Content))
	}
	resource := result.Content[1].Resource
	if resource == nil {
		t.Fatalf("resource content missing: %+v", result.Content[1])
	}
	if resource.URI != "eshu://tool-result/envelope" || resource.MimeType != query.EnvelopeMIMEType {
		t.Fatalf("resource = %+v, want canonical envelope resource", resource)
	}
	if !strings.Contains(resource.Text, "visualization_packet") {
		t.Fatalf("resource text = %q, want visualization_packet payload", resource.Text)
	}
}

func mountVisualizationHandlerForMCP() http.Handler {
	mux := http.NewServeMux()
	router := &query.APIRouter{Visualization: &query.VisualizationHandler{}}
	router.Mount(mux)
	return mux
}

func evidenceCitationSource() map[string]any {
	return map[string]any{
		"question": "why?",
		"citations": []map[string]any{
			{
				"citation_id":     "citation:entity-1",
				"rank":            1,
				"kind":            "entity",
				"evidence_family": "source",
				"entity_id":       "entity-1",
				"entity_name":     "handler",
				"excerpt":         "omitted from visualization nodes",
			},
		},
		"missing_handles": []map[string]any{},
		"coverage": map[string]any{
			"resolved_count": 1,
			"missing_count":  0,
			"limit":          10,
			"truncated":      false,
		},
	}
}

func mustRawMessage(t *testing.T, value any) json.RawMessage {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal raw message: %v", err)
	}
	return encoded
}
