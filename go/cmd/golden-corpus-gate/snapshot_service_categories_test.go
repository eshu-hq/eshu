// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

func TestGoldenServiceCategoriesRejectDuplicateReplacement(t *testing.T) {
	t.Parallel()
	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	shape := snapshot.QueryShapes.MCP["get_service_changed_since"]
	pins := shape.RequiredJSONObjectMatches["data.categories[]"]
	if len(pins) < 3 {
		t.Fatal("ownership, deployment, and runtime evidence pins are missing")
	}
	categories := append([]map[string]any(nil), pins[:3]...)
	for _, category := range []string{"dependencies", "docs", "incidents", "vulnerabilities"} {
		categories = append(categories, map[string]any{
			"category": category, "counts": map[string]any{}, "unavailable": false,
		})
	}
	check := func(rows []map[string]any) bool {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"data": map[string]any{
				"service_id":                   "component:default/deployable-config",
				"since_generation_id":          goldenServiceChangedSincePriorSentinel,
				"current_active_generation_id": goldenServiceChangedSinceCurrentSentinel,
				"sample_limit":                 10,
				"unavailable":                  false,
				"categories":                   rows,
			},
			"truth": map[string]any{"basis": "semantic_facts", "freshness": map[string]any{"state": "fresh"}},
			"error": nil,
		})
		if err != nil {
			t.Fatal(err)
		}
		return goldengate.EvaluateQueryShape("mcp:get_service_changed_since", shape, body).OK
	}
	if !check(categories) {
		t.Fatal("complete seven-category positive control must pass")
	}
	// #7816: the Neo4j first-drain shape before the runtime fence. The prior
	// generation was committed from a runtime read taken before the
	// WorkloadInstance MERGE, so both instances read as added.
	preFence := append([]map[string]any(nil), categories...)
	preFence[2] = map[string]any{
		"category": "runtime",
		"counts": map[string]any{
			"added": 2, "updated": 0, "unchanged": 0, "retired": 0, "superseded": 0,
		},
		"unavailable": false,
	}
	if check(preFence) {
		t.Fatal("a runtime category with added=2 (prior generation read before the WorkloadInstance write) must fail the pin")
	}
	for index, row := range categories {
		t.Run(fmt.Sprint(row["category"]), func(t *testing.T) {
			mutated := append([]map[string]any(nil), categories...)
			mutated[index] = maps.Clone(row)
			mutated[index]["category"] = categories[(index+1)%len(categories)]["category"]
			if check(mutated) {
				t.Fatal("a missing category replaced by a duplicate must fail")
			}
		})
	}
}
