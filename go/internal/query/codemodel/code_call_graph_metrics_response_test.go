// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"testing"
)

// TestCallGraphMetricsResponseEnvelope proves the envelope carries the
// metric shape, the rank sequence, and the truncation probe state.
func TestCallGraphMetricsResponseEnvelope(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"function_key": "k1", "function_id": "f1", "function_name": "main",
		"repo_id": "repo-1", "file_path": "a.go",
	}
	response := CallGraphMetricsResponse(CallGraphMetricsRequest{RepoID: "repo-1"}, []map[string]any{row})

	if response["metric_type"] != "hub_functions" {
		t.Errorf("metric_type = %v, want hub_functions", response["metric_type"])
	}
	functions, ok := response["functions"].([]map[string]any)
	if !ok || len(functions) != 1 {
		t.Fatalf("functions envelope wrong: %#v", response["functions"])
	}
	if functions[0]["rank"] != 1 {
		t.Errorf("rank = %v, want 1", functions[0]["rank"])
	}
	if functions[0]["entity_handle"] != "entity:k1" {
		t.Errorf("entity_handle = %v, want entity:k1", functions[0]["entity_handle"])
	}
	if _, ok := functions[0]["function_key"]; ok {
		t.Error("internal function_key must not leak into the response")
	}
	if response["count"] != 1 || response["truncated"] != false {
		t.Errorf("count/truncated = %v/%v, want 1/false", response["count"], response["truncated"])
	}
}

// TestCallGraphMetricsResponseTruncates proves the limit+1 probe clips
// the page and publishes the next cursor.
func TestCallGraphMetricsResponseTruncates(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"function_key": "k1"}, {"function_key": "k2"}, {"function_key": "k3"},
	}
	limit := 2
	response := CallGraphMetricsResponse(CallGraphMetricsRequest{Limit: &limit}, rows)
	if response["truncated"] != true {
		t.Errorf("over-limit response must be truncated: %#v", response)
	}
	if response["next_offset"] != 2 {
		t.Errorf("next_offset = %v, want 2", response["next_offset"])
	}
	if got := response["count"]; got != 2 {
		t.Errorf("count = %v, want the clipped 2", got)
	}
}

// TestCallGraphMetricRecursionKind proves self calls and mutual calls
// classify apart, with the identity falling back to the legacy key.
func TestCallGraphMetricRecursionKind(t *testing.T) {
	t.Parallel()

	self := map[string]any{"function_key": "k", "partner_key": "k"}
	if got := callGraphRecursionKind(self); got != "self_call" {
		t.Errorf("kind = %q, want self_call", got)
	}
	legacy := map[string]any{"function_id": "f", "partner_id": "g"}
	if got := callGraphRecursionKind(legacy); got != "mutual_call" {
		t.Errorf("kind = %q, want mutual_call", got)
	}
	if got := CallGraphMetricIdentity(legacy, "partner_key", "partner_id"); got != "g" {
		t.Errorf("legacy identity = %q, want g", got)
	}

	evidence := callGraphRecursionEvidence(map[string]any{
		"function_name": "a", "partner_name": "b", "partner_file": "b.go",
		"repo_id": "repo-1", "partner_key": "k2",
	})
	if evidence["relationship_type"] != "CALLS" {
		t.Errorf("recursion evidence must name CALLS: %#v", evidence)
	}
	if _, ok := evidence["partner_source_handle"]; !ok {
		t.Error("recursion evidence must carry the partner source handle")
	}
}
