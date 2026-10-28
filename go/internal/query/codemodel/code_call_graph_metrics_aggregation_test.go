// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"testing"
)

func callGraphMetricsTestRow(sourceUID, targetUID string, sourceLines int) map[string]any {
	return map[string]any{
		"source_uid":        sourceUID,
		"source_id":         "id-" + sourceUID,
		"source_path":       "a.go",
		"source_language":   "go",
		"source_name":       "Func" + sourceUID,
		"source_start_line": sourceLines,
		"source_end_line":   sourceLines + 10,
		"target_uid":        targetUID,
		"target_id":         "id-" + targetUID,
		"target_path":       "b.go",
		"target_language":   "go",
		"target_name":       "Func" + targetUID,
		"target_start_line": 1,
		"target_end_line":   5,
	}
}

func callGraphMetricsHubRequest() CallGraphMetricsRequest {
	return CallGraphMetricsRequest{RepoID: "repo-1"}
}

// TestCallGraphMetricsRowsRanksHubsFirst proves the hub ranking orders
// functions by total degree and reports the expanded scan stats.
func TestCallGraphMetricsRowsRanksHubsFirst(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		callGraphMetricsTestRow("hub", "leaf-a", 10),
		callGraphMetricsTestRow("hub", "leaf-b", 10),
		callGraphMetricsTestRow("leaf-a", "hub", 20),
	}
	rows, stats := CallGraphMetricsRowsWithStats(callGraphMetricsHubRequest(), edges)

	if stats.ExpandedNodes != 3 {
		t.Errorf("ExpandedNodes = %d, want 3", stats.ExpandedNodes)
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if got := rows[0]["function_key"]; got != "hub" {
		t.Errorf("rows[0].function_key = %v, want hub", got)
	}
	if got := rows[0]["total_degree"]; got != 3 {
		t.Errorf("hub total_degree = %v, want 3", got)
	}
	if got := rows[0]["incoming_calls"]; got != 1 {
		t.Errorf("hub incoming_calls = %v, want 1", got)
	}
}

// TestCallGraphMetricsRowsRecursive proves the recursive family reports
// mutual-call pairs and drops one-directional edges.
func TestCallGraphMetricsRowsRecursive(t *testing.T) {
	t.Parallel()

	req := CallGraphMetricsRequest{RepoID: "repo-1", MetricType: "recursive_functions"}
	edges := []map[string]any{
		callGraphMetricsTestRow("x", "y", 10),
		callGraphMetricsTestRow("y", "x", 30),
		callGraphMetricsTestRow("x", "solo", 10),
	}
	rows := CallGraphMetricsRows(req, edges)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 mutual pair", len(rows))
	}
	if got := rows[0]["partner_key"]; got != "y" {
		t.Errorf("partner_key = %v, want y", got)
	}
}

// TestCallGraphMetricsRowsLanguageFilter proves the language filter
// drops functions in other languages before ranking.
func TestCallGraphMetricsRowsLanguageFilter(t *testing.T) {
	t.Parallel()

	req := CallGraphMetricsRequest{RepoID: "repo-1", Language: "python"}
	rows := CallGraphMetricsRows(req, []map[string]any{
		callGraphMetricsTestRow("hub", "leaf", 10),
	})
	if len(rows) != 0 {
		t.Errorf("len(rows) = %d, want 0 for a filtered-out language", len(rows))
	}
}

// TestCallGraphMetricsRowsPaging proves the offset window clips the
// ranking and an offset past the end yields no rows.
func TestCallGraphMetricsRowsPaging(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		callGraphMetricsTestRow("hub", "leaf-a", 10),
		callGraphMetricsTestRow("hub", "leaf-b", 10),
	}
	limit := 1
	req := CallGraphMetricsRequest{RepoID: "repo-1", Limit: &limit, Offset: 1}
	rows := CallGraphMetricsRows(req, edges)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want the limit+1 probe window 2", len(rows))
	}

	pastEnd := CallGraphMetricsRequest{RepoID: "repo-1", Offset: 99}
	if got := CallGraphMetricsRows(pastEnd, edges); len(got) != 0 {
		t.Errorf("offset past the end returned %d rows, want 0", len(got))
	}
}

// TestCallGraphMetricsRequestValidate proves the request bounds reject
// a missing repo, an unknown metric, and out-of-range paging.
func TestCallGraphMetricsRequestValidate(t *testing.T) {
	t.Parallel()

	if err := (CallGraphMetricsRequest{}).Validate(); err == nil {
		t.Error("empty request must fail validation")
	}
	if err := (CallGraphMetricsRequest{RepoID: "r", MetricType: "nope"}).Validate(); err == nil {
		t.Error("unknown metric_type must fail validation")
	}
	over := 500
	if err := (CallGraphMetricsRequest{RepoID: "r", Limit: &over}).Validate(); err == nil {
		t.Error("over-limit request must fail validation")
	}
	zero := 0
	if err := (CallGraphMetricsRequest{RepoID: "r", Limit: &zero}).Validate(); err == nil {
		t.Error("zero limit must fail validation")
	}
	if err := (CallGraphMetricsRequest{RepoID: "r", Offset: -1}).Validate(); err == nil {
		t.Error("negative offset must fail validation")
	}
	if err := (CallGraphMetricsRequest{RepoID: "r"}).Validate(); err != nil {
		t.Errorf("minimal valid request failed validation: %v", err)
	}

	req := CallGraphMetricsRequest{}
	if got := req.EffectiveMetricType(); got != "hub_functions" {
		t.Errorf("default EffectiveMetricType = %q, want hub_functions", got)
	}
	if got := len(CallGraphMetricsRows(CallGraphMetricsRequest{RepoID: "r"}, nil)); got != 0 {
		t.Errorf("empty scan must rank no rows, got %d", got)
	}
}
