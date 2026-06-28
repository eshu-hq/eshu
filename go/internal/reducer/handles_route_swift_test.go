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
			},
			"vapor",
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
	if got, want := payloadStr(intent.Payload, "framework"), "vapor"; got != want {
		t.Fatalf("framework = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "function_entity_id"), "content-entity:health"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "path"), "/health"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "resolution_method"), codeprovenance.MethodSameFile; got != want {
		t.Fatalf("resolution_method = %q, want %q", got, want)
	}
}
