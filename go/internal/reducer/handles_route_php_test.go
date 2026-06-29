// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildHandlesRouteIntentRowsEmitsPHPSymfonyRouteMatches(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"src/Controller/ReportController.php",
			[]map[string]any{
				{
					"name":          "show",
					"class_context": "ReportController",
					"uid":           "content-entity:report-show",
					"line_number":   9,
					"end_line":      11,
					"lang":          "php",
				},
			},
			"symfony",
			[]any{
				map[string]any{"method": "GET", "path": "/reports/{id}", "handler": "ReportController.show"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
	}
	intent := intents[0]
	if got, want := payloadStr(intent.Payload, "function_entity_id"), "content-entity:report-show"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "framework"), "symfony"; got != want {
		t.Fatalf("framework = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "path"), "/reports/{id}"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "http_method"), "GET"; got != want {
		t.Fatalf("http_method = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "resolution_method"), codeprovenance.MethodSameFile; got != want {
		t.Fatalf("resolution_method = %q, want %q", got, want)
	}
}
