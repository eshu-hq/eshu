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

func TestBuildHandlesRouteIntentRowsEmitsPHPSlimRouteMatches(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"app/routes.php",
			[]map[string]any{
				{
					"name":          "handleHome",
					"class_context": "",
					"uid":           "content-entity:slim-handler",
					"line_number":   5,
					"end_line":      7,
					"lang":          "php",
				},
			},
			"slim",
			[]any{
				map[string]any{"method": "GET", "path": "/", "handler": "handleHome"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
	}
	intent := intents[0]
	if got, want := payloadStr(intent.Payload, "function_entity_id"), "content-entity:slim-handler"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "framework"), "slim"; got != want {
		t.Fatalf("framework = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "path"), "/"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "http_method"), "GET"; got != want {
		t.Fatalf("http_method = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "resolution_method"), codeprovenance.MethodSameFile; got != want {
		t.Fatalf("resolution_method = %q, want %q", got, want)
	}
}

func TestBuildHandlesRouteIntentRowsEmitsPHPLaravelAtJoinedRouteMatches(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"routes/web.php",
			[]map[string]any{
				{
					"name":          "index",
					"class_context": "UserController",
					"uid":           "content-entity:user-index",
					"line_number":   8,
					"end_line":      10,
					"lang":          "php",
				},
			},
			"laravel",
			[]any{
				map[string]any{"method": "GET", "path": "/users", "handler": "UserController@index"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
	}
	intent := intents[0]
	if got, want := payloadStr(intent.Payload, "function_entity_id"), "content-entity:user-index"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "framework"), "laravel"; got != want {
		t.Fatalf("framework = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "resolution_method"), codeprovenance.MethodSameFile; got != want {
		t.Fatalf("resolution_method = %q, want %q", got, want)
	}
}

func TestBuildHandlesRouteIntentRowsEmitsPHPLaravelNamespacedAtJoinedRouteMatches(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"routes/web.php",
			[]map[string]any{
				{
					"name":          "index",
					"class_context": "UserController",
					"uid":           "content-entity:user-index",
					"line_number":   8,
					"end_line":      10,
					"lang":          "php",
				},
			},
			"laravel",
			[]any{
				map[string]any{
					"method":  "GET",
					"path":    "/users",
					"handler": `App\Http\Controllers\UserController@index`,
				},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
	}
	if got, want := payloadStr(intents[0].Payload, "function_entity_id"), "content-entity:user-index"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
}

func TestBuildHandlesRouteIntentRowsDoesNotBareMatchWrongLaravelController(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"routes/web.php",
			[]map[string]any{
				{
					"name":          "index",
					"class_context": "UserController",
					"uid":           "content-entity:user-index",
					"line_number":   8,
					"end_line":      10,
					"lang":          "php",
				},
			},
			"laravel",
			[]any{
				map[string]any{"method": "GET", "path": "/users", "handler": "MissingController@index"},
			},
		),
	}

	if intents := buildHandlesRouteIntentsForTest(t, envelopes); len(intents) != 0 {
		t.Fatalf("expected no HANDLES_ROUTE intent for a mismatched controller, got %#v", intents)
	}
}

func TestBuildHandlesRouteIntentRowsDoesNotNormalizeAtJoinedHandlerOutsideLaravel(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"routes/web.php",
			[]map[string]any{
				{
					"name":          "index",
					"class_context": "UserController",
					"uid":           "content-entity:user-index",
					"line_number":   8,
					"end_line":      10,
					"lang":          "php",
				},
			},
			"custom",
			[]any{
				map[string]any{"method": "GET", "path": "/users", "handler": "UserController@index"},
			},
		),
	}

	if intents := buildHandlesRouteIntentsForTest(t, envelopes); len(intents) != 0 {
		t.Fatalf("expected non-Laravel @-joined handler to remain unresolved, got %#v", intents)
	}
}
