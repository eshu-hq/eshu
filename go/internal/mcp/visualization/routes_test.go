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
	sourceTruth := map[string]any{"level": "exact"}
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
	body, ok := request.Body.(map[string]any)
	if !ok {
		t.Fatalf("Route(derive_visualization_packet) body type = %T, want map[string]any", request.Body)
	}
	returnedResponse, ok := body["source_response"].([]any)
	if !ok {
		t.Fatalf("source_response type = %T, want []any", body["source_response"])
	}
	returnedTruth, ok := body["source_truth"].(map[string]any)
	if !ok {
		t.Fatalf("source_truth type = %T, want map[string]any", body["source_truth"])
	}
	returnedResponse[0] = "mutated through route"
	returnedTruth["level"] = "mutated through route"
	if got, want := sourceResponse[0], "mutated through route"; got != want {
		t.Fatalf("source_response alias value = %#v, want %#v", got, want)
	}
	if got, want := sourceTruth["level"], "mutated through route"; got != want {
		t.Fatalf("source_truth alias value = %#v, want %#v", got, want)
	}
}

func TestRoutePreservesEmptyVisualizationArguments(t *testing.T) {
	t.Parallel()

	request, handled := Route("derive_visualization_packet", nil)
	if !handled {
		t.Fatal("Route(derive_visualization_packet) handled = false, want true")
	}
	want := routecontract.Request{
		Method: "POST",
		Path:   "/api/v0/visualizations/derive",
		Body: map[string]any{
			"view":            "",
			"source_response": nil,
			"source_truth":    nil,
		},
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("Route(derive_visualization_packet, nil) request = %#v, want %#v", request, want)
	}

	var sourceResponse map[string]any
	var sourceTruth []any
	typedNilRequest, handled := Route("derive_visualization_packet", routecontract.Arguments{
		"source_response": sourceResponse,
		"source_truth":    sourceTruth,
	})
	if !handled {
		t.Fatal("Route(derive_visualization_packet, typed nils) handled = false, want true")
	}
	typedNilWant := routecontract.Request{
		Method: "POST",
		Path:   "/api/v0/visualizations/derive",
		Body: map[string]any{
			"view":            "",
			"source_response": sourceResponse,
			"source_truth":    sourceTruth,
		},
	}
	if !reflect.DeepEqual(typedNilRequest, typedNilWant) {
		t.Fatalf("Route(derive_visualization_packet, typed nils) request = %#v, want %#v", typedNilRequest, typedNilWant)
	}
}

func TestRoutePreservesExactVisualizationMembership(t *testing.T) {
	t.Parallel()

	for _, toolName := range []string{
		"",
		"ask",
		"derive_visualization_packet_extra",
		"derive_visualization_packet/extra",
		"Derive_visualization_packet",
	} {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			t.Parallel()

			request, handled := Route(toolName, routecontract.Arguments{
				"view": "must not leak into another route",
			})
			if handled {
				t.Fatalf("Route(%q) handled = true, want false", toolName)
			}
			if !reflect.DeepEqual(request, routecontract.Request{}) {
				t.Fatalf("Route(%q) request = %#v, want zero value", toolName, request)
			}
		})
	}
}
