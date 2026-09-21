// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
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
	want := &routecontract.Request{
		Method: "POST",
		Path:   "/api/v0/visualizations/derive",
		Body: map[string]any{
			"view":            "service_story",
			"source_response": sourceResponse,
			"source_truth":    sourceTruth,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveRoute() = %#v, want %#v", got, want)
	}
}
