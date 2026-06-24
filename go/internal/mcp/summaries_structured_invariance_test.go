// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestToolCallStructuredContentUnchanged asserts that the tool-aware text
// summary layer changes only the text block: the structured content and the
// embedded resource block remain byte-identical to the canonical envelope that
// the handler produced. The envelope is the source of truth; text is a
// convenience layer.
func TestToolCallStructuredContentUnchanged(t *testing.T) {
	envelopeBody := map[string]any{
		"data": map[string]any{
			"service_identity": map[string]any{
				"service_name": "payments-api",
				"limitations":  []any{"identity_only materialization"},
			},
			"api_surface":   map[string]any{"endpoint_count": float64(8), "truncated": true},
			"result_limits": map[string]any{"upstream_count": float64(2), "downstream_count": float64(4)},
			"answer_packet": map[string]any{
				"prompt_family": "service.story",
				"primary_tool":  "get_service_story",
				"truth_class":   "deterministic",
				"supported":     true,
				"partial":       false,
			},
		},
		"truth": map[string]any{
			"level":     "exact",
			"freshness": map[string]any{"state": "fresh"},
		},
		"error": nil,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/services/{name}/story", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(envelopeBody)
	})

	s := NewServer(mux, slog.New(slog.NewJSONHandler(io.Discard, nil)))

	req := &jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      float64(7),
		Method:  "tools/call",
	}
	params, err := json.Marshal(map[string]any{
		"name":      "get_service_story",
		"arguments": map[string]any{"workload_id": "payments-api"},
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	req.Params = params

	resp := s.handleMessage(context.Background(), req, "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("handleMessage returned error: %+v", resp)
	}
	result, ok := resp.Result.(mcpToolResult)
	if !ok {
		t.Fatalf("result type = %T, want mcpToolResult", resp.Result)
	}

	// The structured content must be the canonical envelope.
	envelope, ok := result.StructuredContent.(*query.ResponseEnvelope)
	if !ok {
		t.Fatalf("structured content type = %T, want *query.ResponseEnvelope", result.StructuredContent)
	}

	// Re-marshal the canonical envelope and compare against the resource block.
	canonical, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal canonical envelope: %v", err)
	}

	var textBlock, resourceText string
	for _, content := range result.Content {
		switch content.Type {
		case "text":
			textBlock = content.Text
		case "resource":
			if content.Resource == nil {
				t.Fatal("resource block missing resource payload")
			}
			resourceText = content.Resource.Text
			if content.Resource.MimeType != query.EnvelopeMIMEType {
				t.Fatalf("resource mime = %q, want %q", content.Resource.MimeType, query.EnvelopeMIMEType)
			}
		}
	}

	if resourceText != string(canonical) {
		t.Fatalf("resource block drifted from canonical envelope:\n got: %s\nwant: %s", resourceText, canonical)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	packet, ok := data["answer_packet"].(map[string]any)
	if !ok {
		t.Fatalf("data.answer_packet = %#v, want object", data["answer_packet"])
	}
	if got, want := packet["primary_tool"], "get_service_story"; got != want {
		t.Fatalf("answer_packet.primary_tool = %#v, want %#v", got, want)
	}

	// The text block must be the tool-aware summary, not the canonical envelope,
	// and must not equal the structured JSON.
	if textBlock == string(canonical) {
		t.Fatal("text block must not equal the canonical structured content")
	}
	if want := summarizeToolText("get_service_story", envelope); textBlock != want {
		t.Fatalf("text block = %q, want tool-aware summary %q", textBlock, want)
	}
}
