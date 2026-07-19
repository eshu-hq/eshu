// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Java's framework_routes.go/spring_routes.go emit route_entries with a bare
// method-name handler (route.handler = name), not a "Class.method" qualified
// name like Scala or PHP. These tests use route_entries shaped as []any of
// map[string]any -- the realistic shape after a Postgres JSON round-trip --
// because mapSlice() does not decode the parser's raw []map[string]string
// (#5333).

func TestBuildHandlesRouteIntentRowsEmitsJavaSpringRouteMatches(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"src/main/java/example/ReportController.java",
			[]map[string]any{
				{
					"name":          "get",
					"class_context": "ReportController",
					"uid":           "content-entity:report-get",
					"line_number":   14,
					"end_line":      17,
					"lang":          "java",
				},
			},
			"spring",
			[]any{
				map[string]any{"method": "GET", "path": "/api/reports/{id}", "handler": "get"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
	}
	intent := intents[0]
	if got, want := payloadStr(intent.Payload, "function_entity_id"), "content-entity:report-get"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "framework"), "spring"; got != want {
		t.Fatalf("framework = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "path"), "/api/reports/{id}"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "http_method"), "GET"; got != want {
		t.Fatalf("http_method = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "resolution_method"), codeprovenance.MethodSameFile; got != want {
		t.Fatalf("resolution_method = %q, want %q", got, want)
	}
}

func TestBuildHandlesRouteIntentRowsEmitsJavaJAXRSRouteMatches(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"src/main/java/example/WidgetResource.java",
			[]map[string]any{
				{
					"name":          "list",
					"class_context": "WidgetResource",
					"uid":           "content-entity:widget-list",
					"line_number":   20,
					"end_line":      23,
					"lang":          "java",
				},
			},
			"jax_rs",
			[]any{
				map[string]any{"method": "GET", "path": "/widgets", "handler": "list"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
	}
	intent := intents[0]
	if got, want := payloadStr(intent.Payload, "function_entity_id"), "content-entity:widget-list"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "framework"), "jax_rs"; got != want {
		t.Fatalf("framework = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "path"), "/widgets"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "http_method"), "GET"; got != want {
		t.Fatalf("http_method = %q, want %q", got, want)
	}
}

func TestBuildHandlesRouteIntentRowsEmitsJavaMicronautRouteMatches(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"src/main/java/example/PingController.java",
			[]map[string]any{
				{
					"name":          "ping",
					"class_context": "PingController",
					"uid":           "content-entity:micronaut-ping",
					"line_number":   8,
					"end_line":      10,
					"lang":          "java",
				},
			},
			"micronaut",
			[]any{
				map[string]any{"method": "GET", "path": "/ping", "handler": "ping"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
	}
	intent := intents[0]
	if got, want := payloadStr(intent.Payload, "function_entity_id"), "content-entity:micronaut-ping"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "framework"), "micronaut"; got != want {
		t.Fatalf("framework = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "path"), "/ping"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "http_method"), "GET"; got != want {
		t.Fatalf("http_method = %q, want %q", got, want)
	}
}

// TestBuildHandlesRouteIntentRowsSkipsAmbiguousJavaHandler proves java.md:80's
// documented limitation: a bare method-name handler that is not unique within
// the route file and not unique repo-wide must not produce an edge, because a
// guessed binding would corrupt graph truth (#2721).
func TestBuildHandlesRouteIntentRowsSkipsAmbiguousJavaHandler(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"src/main/java/example/routes/Routes.java",
			nil,
			"spring",
			[]any{
				map[string]any{"method": "GET", "path": "/ambiguous", "handler": "handle"},
			},
		),
		{
			FactKind: "file",
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"relative_path": "src/main/java/example/a/AController.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/a/AController.java",
					"functions": []map[string]any{
						{"name": "handle", "class_context": "AController", "uid": "content-entity:a-handle", "line_number": 1, "end_line": 2},
					},
				},
			},
		},
		{
			FactKind: "file",
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"relative_path": "src/main/java/example/b/BController.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/b/BController.java",
					"functions": []map[string]any{
						{"name": "handle", "class_context": "BController", "uid": "content-entity:b-handle", "line_number": 1, "end_line": 2},
					},
				},
			},
		},
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no HANDLES_ROUTE intent for ambiguous Java handler, got %d: %#v", len(intents), intents)
	}
}

// TestBuildHandlesRouteIntentRowsSkipsUnknownJavaHandler proves an entry whose
// handler name resolves to zero Function entities is skipped rather than
// invented.
func TestBuildHandlesRouteIntentRowsSkipsUnknownJavaHandler(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"src/main/java/example/ReportController.java",
			[]map[string]any{
				{
					"name":          "get",
					"class_context": "ReportController",
					"uid":           "content-entity:report-get",
					"line_number":   14,
					"end_line":      17,
					"lang":          "java",
				},
			},
			"spring",
			[]any{
				map[string]any{"method": "GET", "path": "/api/reports/{id}", "handler": "doesNotExist"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no HANDLES_ROUTE intent for unknown Java handler, got %d", len(intents))
	}
}
