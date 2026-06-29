// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildHandlesRouteIntentRowsEmitsCPPExactEntries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		framework string
		handler   string
		function  map[string]any
		method    string
		routePath string
	}{
		{
			name:      "crow free function",
			framework: "crow",
			handler:   "health",
			function:  map[string]any{"name": "health", "uid": "content-entity:health", "line_number": 10, "end_line": 12},
			method:    "GET",
			routePath: "/health",
		},
		{
			name:      "pistache class method",
			framework: "pistache",
			handler:   "OrdersController.show",
			function: map[string]any{
				"name":          "show",
				"class_context": "OrdersController",
				"uid":           "content-entity:orders-show",
				"line_number":   20,
				"end_line":      24,
			},
			method:    "GET",
			routePath: "/orders/:id",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{
				handlesRouteRepoEnvelope("repo-1"),
				handlesRouteFileEnvelope(
					"repo-1",
					"src/routes.cpp",
					[]map[string]any{tc.function},
					tc.framework,
					[]any{
						map[string]any{"method": tc.method, "path": tc.routePath, "handler": tc.handler},
					},
				),
			}

			intents := buildHandlesRouteIntentsForTest(t, envelopes)
			if len(intents) != 1 {
				t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
			}
			intent := intents[0]
			if got, want := payloadStr(intent.Payload, "framework"), tc.framework; got != want {
				t.Fatalf("framework = %q, want %q", got, want)
			}
			if got, want := payloadStr(intent.Payload, "path"), tc.routePath; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			if got, want := payloadStr(intent.Payload, "http_method"), tc.method; got != want {
				t.Fatalf("http_method = %q, want %q", got, want)
			}
		})
	}
}
