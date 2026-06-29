// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildHandlesRouteIntentRowsEmitsSwiftVaporRouteMatches(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"Sources/App/Routes.swift",
			[]map[string]any{
				{"name": "health", "uid": "content-entity:health", "line_number": 8, "end_line": 10},
				{"name": "listUsers", "uid": "content-entity:listUsers", "line_number": 12, "end_line": 14},
			},
			"vapor",
			[]any{
				map[string]any{"method": "GET", "path": "/health", "handler": "health"},
				map[string]any{"method": "GET", "path": "/api/users", "handler": "listUsers"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 2 {
		t.Fatalf("expected exactly 2 HANDLES_ROUTE intents, got %d", len(intents))
	}
	byPath := make(map[string]SharedProjectionIntentRow, len(intents))
	for _, intent := range intents {
		if got, want := payloadStr(intent.Payload, "framework"), "vapor"; got != want {
			t.Fatalf("framework = %q, want %q", got, want)
		}
		if got, want := payloadStr(intent.Payload, "resolution_method"), codeprovenance.MethodSameFile; got != want {
			t.Fatalf("resolution_method = %q, want %q", got, want)
		}
		byPath[payloadStr(intent.Payload, "path")] = intent
	}

	intent, ok := byPath["/health"]
	if !ok {
		t.Fatalf("missing /health intent in %#v", intents)
	}
	if got, want := payloadStr(intent.Payload, "function_entity_id"), "content-entity:health"; got != want {
		t.Fatalf("/health function_entity_id = %q, want %q", got, want)
	}

	grouped, ok := byPath["/api/users"]
	if !ok {
		t.Fatalf("missing /api/users intent in %#v", intents)
	}
	if got, want := payloadStr(grouped.Payload, "function_entity_id"), "content-entity:listUsers"; got != want {
		t.Fatalf("/api/users function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(grouped.Payload, "http_method"), "GET"; got != want {
		t.Fatalf("/api/users http_method = %q, want %q", got, want)
	}
}
