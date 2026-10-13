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
	if len(pins) < 2 {
		t.Fatal("ownership and deployment evidence pins are missing")
	}
	categories := append([]map[string]any(nil), pins[:2]...)
	for _, category := range []string{"runtime", "dependencies", "docs", "incidents", "vulnerabilities"} {
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
