// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildHandlesRouteIntentRowsEmitsAioHTTPTornadoFrameworkRoutes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		relative   string
		framework  string
		functions  []map[string]any
		entries    []any
		wantEntity string
		wantPath   string
		wantMethod string
	}{
		{
			name:      "aiohttp function handler",
			relative:  "routes.py",
			framework: "aiohttp",
			functions: []map[string]any{
				{"name": "list_widgets", "uid": "content-entity:aiohttp-list", "line_number": 10, "end_line": 12, "lang": "python"},
			},
			entries: []any{
				map[string]any{"method": "GET", "path": "/widgets", "handler": "list_widgets"},
			},
			wantEntity: "content-entity:aiohttp-list",
			wantPath:   "/widgets",
			wantMethod: "GET",
		},
		{
			name:      "tornado class method handler",
			relative:  "handlers.py",
			framework: "tornado",
			functions: []map[string]any{
				{"name": "post", "class_context": "WidgetHandler", "uid": "content-entity:tornado-post", "line_number": 20, "end_line": 25, "lang": "python"},
			},
			entries: []any{
				map[string]any{"method": "POST", "path": "/widgets", "handler": "WidgetHandler.post"},
			},
			wantEntity: "content-entity:tornado-post",
			wantPath:   "/widgets",
			wantMethod: "POST",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{
				handlesRouteRepoEnvelope("repo-1"),
				handlesRouteFileEnvelope("repo-1", tc.relative, tc.functions, tc.framework, tc.entries),
			}

			intents := buildHandlesRouteIntentsForTest(t, envelopes)
			if len(intents) != 1 {
				t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
			}
			intent := intents[0]
			if got, want := payloadStr(intent.Payload, "framework"), tc.framework; got != want {
				t.Fatalf("framework = %q, want %q", got, want)
			}
			if got, want := payloadStr(intent.Payload, "function_entity_id"), tc.wantEntity; got != want {
				t.Fatalf("function_entity_id = %q, want %q", got, want)
			}
			if got, want := payloadStr(intent.Payload, "path"), tc.wantPath; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			if got, want := payloadStr(intent.Payload, "http_method"), tc.wantMethod; got != want {
				t.Fatalf("http_method = %q, want %q", got, want)
			}
		})
	}
}
