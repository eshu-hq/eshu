// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialization

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

func TestBuildHandlesRouteIntentRowsEmitsCSharpASPNetExactEntries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		framework string
		filePath  string
		handler   string
		method    string
		routePath string
	}{
		{
			name:      "attribute routes",
			framework: "aspnet",
			filePath:  "Controllers/OrdersController.cs",
			handler:   "OrdersController.Get",
			method:    "GET",
			routePath: "/api/orders/{id}",
		},
		{
			name:      "minimal api routes",
			framework: "aspnet_minimal_api",
			filePath:  "Program.cs",
			handler:   "Health",
			method:    "GET",
			routePath: "/health",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{
				handlesRouteRepoEnvelope("repo-1"),
				handlesRouteFileEnvelope(
					"repo-1",
					tc.filePath,
					[]map[string]any{
						csharpRouteFunction(tc.handler),
					},
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
			if got, want := payloadcore.PayloadStr(intent.Payload, "framework"), tc.framework; got != want {
				t.Fatalf("framework = %q, want %q", got, want)
			}
			if got, want := payloadcore.PayloadStr(intent.Payload, "function_entity_id"), "content-entity:csharp-handler"; got != want {
				t.Fatalf("function_entity_id = %q, want %q", got, want)
			}
			if got, want := payloadcore.PayloadStr(intent.Payload, "path"), tc.routePath; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			if got, want := payloadcore.PayloadStr(intent.Payload, "http_method"), tc.method; got != want {
				t.Fatalf("http_method = %q, want %q", got, want)
			}
		})
	}
}

func csharpRouteFunction(handler string) map[string]any {
	if handler == "OrdersController.Get" {
		return map[string]any{
			"name":          "Get",
			"class_context": "OrdersController",
			"uid":           "content-entity:csharp-handler",
			"line_number":   10,
			"end_line":      12,
		}
	}
	return map[string]any{
		"name":        handler,
		"uid":         "content-entity:csharp-handler",
		"line_number": 10,
		"end_line":    12,
	}
}
