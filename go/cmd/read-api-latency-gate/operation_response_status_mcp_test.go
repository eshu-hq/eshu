// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"testing"
)

// seededStatusMCPEnvelope renders a tools/call envelope for a status-proxy
// MCP tool (list_ingesters, get_hosted_readiness,
// get_operator_control_plane): the proxied HTTP JSON becomes the tool Value,
// so the transport renders the two-block shape with structuredContent
// mirroring the resource text. The structured payload is declared here,
// independent of the validator's field list.
func seededStatusMCPEnvelope(structured map[string]any) map[string]any {
	raw, err := json.Marshal(structured)
	if err != nil {
		panic(err)
	}
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"result": map[string]any{
			"structuredContent": structured,
			"content": []any{
				map[string]any{"type": "text", "text": "seeded"},
				map[string]any{"type": "resource", "resource": map[string]any{"text": string(raw)}},
			},
		},
	}
}

func seededReadinessStructured() map[string]any {
	return map[string]any{
		"version":          "seed",
		"state":            "ready",
		"ready":            true,
		"summary":          "seeded",
		"repository_count": float64(1),
		"queue":            map[string]any{},
		"checks":           []any{},
	}
}

func seededIngestersListStructured() map[string]any {
	return map[string]any{
		"version":   "seed",
		"ingesters": []any{map[string]any{"name": "repository", "runtime_family": "ingester"}},
		"count":     float64(1),
	}
}

func seededControlPlaneStructured() map[string]any {
	return map[string]any{
		"version":         "seed",
		"as_of":           "2026-01-01T00:00:00Z",
		"health":          map[string]any{},
		"queue":           map[string]any{"outstanding": float64(0)},
		"reducer_domains": []any{},
	}
}

// TestPilotStatusMCPRequiresSeededPayload is the RED/GREEN pair for the three
// #7009 status-proxy MCP Expect contracts: the declared seeded payload
// passes, and a wrong envelope identity, a mirrored-resource mismatch, or a
// payload missing its route identity fails.
func TestPilotStatusMCPRequiresSeededPayload(t *testing.T) {
	t.Parallel()

	structured := map[string]func() map[string]any{
		"mcp_hosted_readiness":      seededReadinessStructured,
		"mcp_ingesters_list":        seededIngestersListStructured,
		"mcp_operator_controlplane": seededControlPlaneStructured,
	}
	for expect := range structured {
		t.Run(expect, func(t *testing.T) {
			t.Parallel()
			body, err := json.Marshal(seededStatusMCPEnvelope(structured[expect]()))
			if err != nil {
				t.Fatal(err)
			}
			if err := validatePilotResponse(expect, body); err != nil {
				t.Fatalf("declared seeded payload rejected: %v", err)
			}
		})
	}
	mutants := map[string]func(map[string]any){
		"wrong_identity": func(envelope map[string]any) {
			envelope["id"] = float64(2)
		},
		"resource_mismatch": func(envelope map[string]any) {
			content := envelope["result"].(map[string]any)["content"].([]any)
			content[1].(map[string]any)["resource"].(map[string]any)["text"] = "{}"
		},
		"missing_route_identity": func(envelope map[string]any) {
			payload := structuredPayload(envelope)
			delete(payload, "state")
			delete(payload, "ingesters")
			delete(payload, "reducer_domains")
		},
	}
	for expect := range structured {
		for name, mutate := range mutants {
			t.Run(expect+"/"+name, func(t *testing.T) {
				envelope := seededStatusMCPEnvelope(structured[expect]())
				mutate(envelope)
				body, err := json.Marshal(envelope)
				if err != nil {
					t.Fatal(err)
				}
				if err := validatePilotResponse(expect, body); err == nil {
					t.Fatal("incorrect status payload passed")
				}
			})
		}
	}
}

// structuredPayload navigates a seeded tools/call envelope to
// structuredContent.
func structuredPayload(envelope map[string]any) map[string]any {
	return envelope["result"].(map[string]any)["structuredContent"].(map[string]any)
}
