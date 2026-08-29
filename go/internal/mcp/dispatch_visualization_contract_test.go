// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"
)

func TestResolveRouteUsesExactVisualizationChildRequest(t *testing.T) {
	t.Parallel()

	sourceResponse := map[string]any{"service_id": "svc-1"}
	sourceTruth := map[string]any{"level": "exact"}
	got, err := resolveRoute("derive_visualization_packet", map[string]any{
		"view":            "service_story",
		"source_response": sourceResponse,
		"source_truth":    sourceTruth,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	want := &route{
		method: "POST",
		path:   "/api/v0/visualizations/derive",
		body: map[string]any{
			"view":            "service_story",
			"source_response": sourceResponse,
			"source_truth":    sourceTruth,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveRoute() = %#v, want %#v", got, want)
	}
}
