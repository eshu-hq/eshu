// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"
)

func TestVisualizationRouteAdaptsExactChildRequest(t *testing.T) {
	t.Parallel()

	sourceResponse := map[string]any{"service_id": "svc-1"}
	sourceTruth := map[string]any{"level": "exact"}
	got, handled := visualizationRoute("derive_visualization_packet", map[string]any{
		"view":            "service_story",
		"source_response": sourceResponse,
		"source_truth":    sourceTruth,
	})
	if !handled {
		t.Fatal("visualizationRoute() handled = false, want true")
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
		t.Fatalf("visualizationRoute() = %#v, want %#v", got, want)
	}
}
