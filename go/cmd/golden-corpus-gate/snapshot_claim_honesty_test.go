// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

func TestGoldenSnapshotMakesThinEvidenceCapabilitiesNonVacuous(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	listShapes := map[string]string{
		"list_secrets_iam_identity_trust_chains":          "identity_trust_chains",
		"list_secrets_iam_privilege_posture_observations": "privilege_posture_observations",
		"list_secrets_iam_secret_access_paths":            "secret_access_paths",
		"list_secrets_iam_posture_gaps":                   "posture_gaps",
	}
	for tool, resultsField := range listShapes {
		shape, ok := snapshot.QueryShapes.MCP[tool]
		if !ok {
			t.Fatalf("query_shapes.mcp missing %s", tool)
		}
		if shape.MinimumResults < 1 {
			t.Errorf("%s minimum_results = %d, want >= 1", tool, shape.MinimumResults)
		}
		if shape.ResultsField != resultsField {
			t.Errorf("%s results_field = %q, want %q", tool, shape.ResultsField, resultsField)
		}
	}

	summary := snapshot.QueryShapes.MCP["count_secrets_iam_posture"]
	for _, path := range []string{
		"summary.identity_trust_chains_by_state[].count",
		"summary.privilege_observations_by_risk_type[].count",
		"summary.secret_access_paths_by_state[].count",
		"summary.posture_gaps_by_gap_type[].count",
	} {
		if _, ok := summary.RequiredJSONValues[path]; !ok {
			t.Errorf("count_secrets_iam_posture missing required_json_values[%q]", path)
		}
	}

	variable := snapshot.QueryShapes.HTTP["POST /api/v0/code/search"]
	if got := variable.RequestBody["search_type"]; got != "variable" {
		t.Errorf("code search request_body[search_type] = %#v, want variable", got)
	}
	if got := variable.RequiredJSONValues["matches[].labels[]"]; got != "Variable" {
		t.Errorf("code search variable label pin = %#v, want Variable", got)
	}

	trace := snapshot.QueryShapes.MCP["trace_exposure_path"]
	for path, want := range map[string]any{
		"source.name":        "list_orders",
		"source_kind":        "http_handler",
		"truth_label":        "derived",
		"state":              "unresolved",
		"coverage.max_depth": float64(4),
	} {
		if got := trace.RequiredJSONValues[path]; got != want {
			t.Errorf("trace_exposure_path required_json_values[%q] = %#v, want %#v", path, got, want)
		}
	}
	if !containsString(trace.RequiredJSONPaths, "coverage.unresolved_reason") {
		t.Error("trace_exposure_path must require a non-empty unresolved reason")
	}
}
