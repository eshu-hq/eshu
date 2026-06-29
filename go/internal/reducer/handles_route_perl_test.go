// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildHandlesRouteIntentRowsEmitsPerlExactEntries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		framework string
	}{
		{name: "mojolicious lite", framework: "mojolicious"},
		{name: "dancer", framework: "dancer"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{
				handlesRouteRepoEnvelope("repo-1"),
				handlesRouteFileEnvelope(
					"repo-1",
					"app.pl",
					[]map[string]any{
						{"name": "health", "uid": "content-entity:health", "line_number": 4, "end_line": 4},
					},
					tc.framework,
					[]any{
						map[string]any{"method": "GET", "path": "/health", "handler": "health"},
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
			if got, want := payloadStr(intent.Payload, "path"), "/health"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			if got, want := payloadStr(intent.Payload, "http_method"), "GET"; got != want {
				t.Fatalf("http_method = %q, want %q", got, want)
			}
		})
	}
}

func TestBuildHandlesRouteIntentRowsResolvesPerlQualifiedHandler(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"app.pl",
			[]map[string]any{
				{"name": "show", "full_name": "Public::show", "uid": "content-entity:public-show", "line_number": 4, "end_line": 4},
				{"name": "show", "full_name": "Admin::show", "uid": "content-entity:admin-show", "line_number": 8, "end_line": 8},
			},
			"dancer",
			[]any{
				map[string]any{"method": "GET", "path": "/orders", "handler": "Admin::show"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
	}
	if got, want := payloadStr(intents[0].Payload, "function_entity_id"), "content-entity:admin-show"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
}
