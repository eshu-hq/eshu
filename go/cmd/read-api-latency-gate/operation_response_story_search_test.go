// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

// seededStoryResponse is a declared story answer for the seeded anchor,
// independent of the validator's field list.
func seededStoryResponse() map[string]any {
	return map[string]any{
		"repository": map[string]any{"id": SeedStoryAnchorID, "name": SeedStoryAnchorName},
		"subject":    map[string]any{"type": "repository", "id": SeedStoryAnchorID},
		"deployment_overview": map[string]any{
			"deployment_artifacts": map[string]any{"delivery_paths": []any{}},
		},
		"story_sections": []any{
			map[string]any{"title": "codebase", "summary": "seeded"},
			map[string]any{"title": "deployment", "summary": "seeded"},
		},
	}
}

// seededSearchEnvelope renders a tools/call envelope for an exact search
// page with two rows, independent of the validator's field list.
func seededSearchEnvelope(rows int) map[string]any {
	results := make([]any, 0, rows)
	for i := 0; i < rows; i++ {
		results = append(results, map[string]any{"repo_id": "seed-search-00"})
	}
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"result": map[string]any{
			"structuredContent": map[string]any{
				"data": map[string]any{
					"results":   results,
					"count":     float64(rows),
					"truncated": true,
				},
				"truth": map[string]any{"level": "exact"},
			},
			"content": []any{
				map[string]any{"type": "text", "text": "seeded"},
				map[string]any{"type": "resource", "resource": map[string]any{"text": "{}"}},
			},
		},
	}
}

// TestPilotStoryRequiresSeededAnchor is the RED/GREEN pair for the story
// Expect contract: the declared seeded answer passes, and a wrong anchor,
// a missing subject, a missing deployment overview, or empty sections fail.
func TestPilotStoryRequiresSeededAnchor(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(seededStoryResponse())
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePilotResponse("repository_story", body); err != nil {
		t.Fatalf("declared seeded story rejected: %v", err)
	}
	mutants := map[string]func(map[string]any){
		"wrong_anchor": func(payload map[string]any) {
			payload["repository"].(map[string]any)["id"] = "other"
		},
		"missing_subject": func(payload map[string]any) {
			delete(payload, "subject")
		},
		"missing_deployment_overview": func(payload map[string]any) {
			delete(payload, "deployment_overview")
		},
		"empty_sections": func(payload map[string]any) {
			payload["story_sections"] = []any{}
		},
	}
	for name, mutate := range mutants {
		t.Run(name, func(t *testing.T) {
			payload := seededStoryResponse()
			mutate(payload)
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if err := validatePilotResponse("repository_story", body); err == nil {
				t.Fatal("incorrect story passed")
			}
		})
	}
}

// TestPilotSearchRequiresExactSeededPage is the RED/GREEN pair for the two
// unscoped-search Expect contracts: the declared exact page passes for both
// tools, and empty results, a budget-cut partial, or a partial truth level
// fail.
func TestPilotSearchRequiresExactSeededPage(t *testing.T) {
	t.Parallel()

	for _, expect := range []string{"mcp_file_search", "mcp_entity_search"} {
		t.Run(expect, func(t *testing.T) {
			t.Parallel()
			body, err := json.Marshal(seededSearchEnvelope(2))
			if err != nil {
				t.Fatal(err)
			}
			if err := validatePilotResponse(expect, body); err != nil {
				t.Fatalf("declared exact search page rejected: %v", err)
			}
		})
	}
	mutants := map[string]func(map[string]any){
		"empty_results": func(envelope map[string]any) {
			structured(envelope)["data"].(map[string]any)["results"] = []any{}
		},
		"budget_cut_partial": func(envelope map[string]any) {
			structured(envelope)["data"].(map[string]any)["partial"] = map[string]any{"reason": "candidate_budget_exceeded"}
		},
		"partial_truth": func(envelope map[string]any) {
			structured(envelope)["truth"].(map[string]any)["level"] = "partial"
		},
		"missing_truth": func(envelope map[string]any) {
			delete(structured(envelope), "truth")
		},
	}
	for _, expect := range []string{"mcp_file_search", "mcp_entity_search"} {
		for name, mutate := range mutants {
			t.Run(fmt.Sprintf("%s/%s", expect, name), func(t *testing.T) {
				envelope := seededSearchEnvelope(2)
				mutate(envelope)
				body, err := json.Marshal(envelope)
				if err != nil {
					t.Fatal(err)
				}
				if err := validatePilotResponse(expect, body); err == nil {
					t.Fatal("incorrect search page passed")
				}
			})
		}
	}
}

// structured navigates a seeded tools/call envelope to structuredContent.
func structured(envelope map[string]any) map[string]any {
	return envelope["result"].(map[string]any)["structuredContent"].(map[string]any)
}
