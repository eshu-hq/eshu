// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

func TestGoldenSnapshotDefaultPreciseSupplyChainImpactFloor(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	const key = "GET /api/v0/supply-chain/impact/findings?limit=50&cve_id=CVE-2026-00000"
	shape, ok := snapshot.QueryShapes.HTTP[key]
	if !ok {
		t.Fatalf("query_shapes.http missing %q", key)
	}
	if shape.MinimumResults != 1 {
		t.Fatalf("%s minimum_results = %d, want 1", key, shape.MinimumResults)
	}
	if shape.MaximumResults != 1 {
		t.Fatalf("%s maximum_results = %d, want 1", key, shape.MaximumResults)
	}
	hasSelectedProfile := false
	for _, field := range shape.RequiredResponseFields {
		if field == "detection_profile" {
			hasSelectedProfile = true
			break
		}
	}
	if !hasSelectedProfile {
		t.Fatalf("%s required_response_fields missing detection_profile", key)
	}
	for _, field := range []string{"cve_id", "detection_profile"} {
		if !containsString(shape.ResultItemRequiredFields, field) {
			t.Fatalf("%s result_item_required_fields = %#v, want %q", key, shape.ResultItemRequiredFields, field)
		}
	}
	for path, want := range map[string]any{
		"detection_profile":            "precise",
		"findings[].cve_id":            "CVE-2026-00000",
		"findings[].detection_profile": "precise",
	} {
		if got := shape.RequiredJSONValues[path]; got != want {
			t.Fatalf("%s required_json_values[%q] = %#v, want %#v", key, path, got, want)
		}
	}
}
