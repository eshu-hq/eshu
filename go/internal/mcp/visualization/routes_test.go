// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualizationtools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

func TestRoutePreservesVisualizationRequestContract(t *testing.T) {
	t.Parallel()

	sourceResponse := map[string]any{
		"service_identity": map[string]any{"service_id": "svc-1"},
	}
	sourceTruth := map[string]any{
		"level": "exact",
		"freshness": map[string]any{
			"state": "fresh",
		},
	}
	request, handled := Route("derive_visualization_packet", routecontract.Arguments{
		"view":            "service_story",
		"source_response": sourceResponse,
		"source_truth":    sourceTruth,
	})
	if !handled {
		t.Fatal("Route(derive_visualization_packet) handled = false, want true")
	}
	want := routecontract.Request{
		Method: "POST",
		Path:   "/api/v0/visualizations/derive",
		Body: map[string]any{
			"view":            "service_story",
			"source_response": sourceResponse,
			"source_truth":    sourceTruth,
		},
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("Route(derive_visualization_packet) request = %#v, want %#v", request, want)
	}
}

func TestRoutePreservesVisualizationCoercionAndPassThrough(t *testing.T) {
	t.Parallel()

	sourceResponse := []any{"unchanged", float64(7)}
	sourceTruth := "unchanged"
	request, handled := Route("derive_visualization_packet", routecontract.Arguments{
		"view":            42,
		"source_response": sourceResponse,
		"source_truth":    sourceTruth,
	})
	if !handled {
		t.Fatal("Route(derive_visualization_packet) handled = false, want true")
	}
	want := routecontract.Request{
		Method: "POST",
		Path:   "/api/v0/visualizations/derive",
		Body: map[string]any{
			"view":            "",
			"source_response": sourceResponse,
			"source_truth":    sourceTruth,
		},
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("Route(derive_visualization_packet) request = %#v, want %#v", request, want)
	}
}

func TestRouteRejectsUnrelatedTool(t *testing.T) {
	t.Parallel()

	request, handled := Route("ask", routecontract.Arguments{
		"view": "must not leak into another route",
	})
	if handled {
		t.Fatal("Route(ask) handled = true, want false")
	}
	if !reflect.DeepEqual(request, routecontract.Request{}) {
		t.Fatalf("Route(ask) request = %#v, want zero value", request)
	}
}
