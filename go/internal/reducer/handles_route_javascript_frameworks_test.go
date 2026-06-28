// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildHandlesRouteIntentRowsEmitsJavaScriptFrameworkRouteMatches(t *testing.T) {
	t.Parallel()

	for _, framework := range []string{"koa", "fastify", "nestjs"} {
		framework := framework
		t.Run(framework, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{
				handlesRouteRepoEnvelope("repo-1"),
				handlesRouteFileEnvelope(
					"repo-1",
					"routes.ts",
					[]map[string]any{
						{"name": "Health", "uid": "content-entity:health", "line_number": 10, "end_line": 20},
					},
					framework,
					[]any{
						map[string]any{"method": "GET", "path": "/health", "handler": "Health"},
					},
				),
			}

			intents := buildHandlesRouteIntentsForTest(t, envelopes)
			if len(intents) != 1 {
				t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
			}
			intent := intents[0]
			if got, want := payloadStr(intent.Payload, "framework"), framework; got != want {
				t.Fatalf("framework = %q, want %q", got, want)
			}
			if got, want := payloadStr(intent.Payload, "function_entity_id"), "content-entity:health"; got != want {
				t.Fatalf("function_entity_id = %q, want %q", got, want)
			}
			if got, want := payloadStr(intent.Payload, "path"), "/health"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			if got, want := payloadStr(intent.Payload, "http_method"), "GET"; got != want {
				t.Fatalf("http_method = %q, want %q", got, want)
			}
		})
	}
}
