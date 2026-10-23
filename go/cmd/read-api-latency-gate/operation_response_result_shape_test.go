// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"testing"
)

// structuredShapeEnvelope renders the one-copy structured shape (#8800): one
// text summary and structuredContent, with no embedded resource.
func structuredShapeEnvelope(structured map[string]any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"result": map[string]any{
			"structuredContent": structured,
			"content":           []any{map[string]any{"type": "text", "text": "seeded"}},
		},
	}
}

// TestPilotStatusMCPAcceptsBothResultShapes is the RED/GREEN pair for the
// shape-aware content check: the dual two-block shape and the structured
// one-block shape both pass, and every other content layout fails.
func TestPilotStatusMCPAcceptsBothResultShapes(t *testing.T) {
	t.Parallel()

	structured := map[string]func() map[string]any{
		"mcp_hosted_readiness":      seededReadinessStructured,
		"mcp_ingesters_list":        seededIngestersListStructured,
		"mcp_operator_controlplane": seededControlPlaneStructured,
	}
	marshal := func(envelope map[string]any) []byte {
		body, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	for expect, build := range structured {
		if err := validatePilotResponse(expect, marshal(structuredShapeEnvelope(build()))); err != nil {
			t.Fatalf("%s: structured shape rejected: %v", expect, err)
		}
		if err := validatePilotResponse(expect, marshal(seededStatusMCPEnvelope(build()))); err != nil {
			t.Fatalf("%s: dual shape rejected: %v", expect, err)
		}
	}

	text := map[string]any{"type": "text", "text": "seeded"}
	resource := func() map[string]any {
		raw, _ := json.Marshal(seededReadinessStructured())
		return map[string]any{"type": "resource", "resource": map[string]any{"text": string(raw)}}
	}
	badContent := map[string][]any{
		"no content":                  {},
		"resource only":               {resource()},
		"empty summary":               {map[string]any{"type": "text", "text": ""}},
		"two text blocks":             {text, text},
		"resource before text":        {resource(), text},
		"three blocks":                {text, resource(), resource()},
		"structured with extra text":  {text, map[string]any{"type": "text", "text": "again"}},
		"unknown single block type":   {map[string]any{"type": "image"}},
		"non-object single content":   {"seeded"},
		"dual with empty resource":    {text, map[string]any{"type": "resource", "resource": map[string]any{}}},
		"dual with non-resource type": {text, map[string]any{"type": "text", "resource": map[string]any{"text": "{}"}}},
	}
	for name, content := range badContent {
		envelope := structuredShapeEnvelope(seededReadinessStructured())
		envelope["result"].(map[string]any)["content"] = content
		if err := validatePilotResponse("mcp_hosted_readiness", marshal(envelope)); err == nil {
			t.Fatalf("%s: malformed content layout passed", name)
		}
	}
}
