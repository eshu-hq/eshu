// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// handlesRouteRepoEnvelope builds a repository fact envelope that anchors the
// projection context (scope, source run, acceptance unit) for a repo.
func handlesRouteRepoEnvelope(repoID string) facts.Envelope {
	return facts.Envelope{
		FactKind: "repository",
		ScopeID:  "scope-1",
		Payload: map[string]any{
			"repo_id":       repoID,
			"source_run_id": "run-1",
		},
	}
}

// handlesRouteFileEnvelope builds a file fact envelope whose parsed_file_data
// carries the given functions and a single-framework route_entries slice.
func handlesRouteFileEnvelope(
	repoID string,
	relativePath string,
	functions []map[string]any,
	framework string,
	routeEntries []any,
) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		ScopeID:  "scope-1",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path":      relativePath,
				"functions": functions,
				"framework_semantics": map[string]any{
					"frameworks": []any{framework},
					framework: map[string]any{
						"route_entries": routeEntries,
					},
				},
			},
		},
	}
}

func buildHandlesRouteIntentsForTest(t *testing.T, envelopes []facts.Envelope) []SharedProjectionIntentRow {
	t.Helper()
	generationID := "gen-1"
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, generationID)
	index := buildCodeEntityIndex(envelopes)
	return buildHandlesRouteIntentRows(
		envelopes,
		index,
		contextByRepoID,
		time.Unix(0, 0).UTC(),
		handlesRouteEvidenceSource,
	)
}

func TestBuildHandlesRouteIntentRowsEmitsExactSameFileMatch(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"server.js",
			[]map[string]any{
				{"name": "getWidgets", "uid": "content-entity:gw", "line_number": 10, "end_line": 20},
			},
			"express",
			[]any{
				map[string]any{"method": "GET", "path": "/widgets", "handler": "getWidgets"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
	}

	intent := intents[0]
	if intent.ProjectionDomain != DomainHandlesRoute {
		t.Fatalf("projection domain = %q, want %q", intent.ProjectionDomain, DomainHandlesRoute)
	}
	if got, want := payloadStr(intent.Payload, "function_entity_id"), "content-entity:gw"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "repo_id"), "repo-1"; got != want {
		t.Fatalf("repo_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "path"), "/widgets"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "http_method"), "GET"; got != want {
		t.Fatalf("http_method = %q, want %q", got, want)
	}
	method := payloadStr(intent.Payload, "resolution_method")
	if !codeprovenance.Classified(method) {
		t.Fatalf("resolution_method = %q, want a classified provenance method", method)
	}
	if method != codeprovenance.MethodSameFile {
		t.Fatalf("resolution_method = %q, want same_file for an in-file handler", method)
	}
}

func TestBuildHandlesRouteIntentRowsEmitsGoFrameworkRouteMatches(t *testing.T) {
	t.Parallel()

	for _, framework := range []string{"net_http", "gin", "echo", "chi", "fiber"} {
		framework := framework
		t.Run(framework, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{
				handlesRouteRepoEnvelope("repo-1"),
				handlesRouteFileEnvelope(
					"repo-1",
					"routes.go",
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

func TestBuildHandlesRouteIntentRowsResolvesRepoUniqueAcrossFiles(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		// Handler defined in a different file than the route registration.
		handlesRouteFileEnvelope(
			"repo-1",
			"routes.js",
			nil,
			"express",
			[]any{
				map[string]any{"method": "POST", "path": "/things", "handler": "createThing"},
			},
		),
		{
			FactKind: "file",
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"relative_path": "handlers.js",
				"parsed_file_data": map[string]any{
					"path": "handlers.js",
					"functions": []map[string]any{
						{"name": "createThing", "uid": "content-entity:ct", "line_number": 5, "end_line": 9},
					},
				},
			},
		},
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
	}
	intent := intents[0]
	if got, want := payloadStr(intent.Payload, "function_entity_id"), "content-entity:ct"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "resolution_method"), codeprovenance.MethodRepoUniqueName; got != want {
		t.Fatalf("resolution_method = %q, want %q", got, want)
	}
}

func TestBuildHandlesRouteIntentRowsPreservesJVMFrameworkProvenance(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"src/main/kotlin/example/JvmRoutes.kt",
			[]map[string]any{
				{"name": "ping", "uid": "content-entity:ping", "line_number": 12, "end_line": 14},
			},
			"ktor",
			[]any{
				map[string]any{"method": "GET", "path": "/ktor/ping", "handler": "ping"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d", len(intents))
	}
	intent := intents[0]
	if got, want := payloadStr(intent.Payload, "framework"), "ktor"; got != want {
		t.Fatalf("framework = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "path"), "/ktor/ping"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "http_method"), "GET"; got != want {
		t.Fatalf("http_method = %q, want %q", got, want)
	}
}

func TestBuildHandlesRouteIntentRowsResolvesClassMethodHandler(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"urls.py",
			[]map[string]any{
				{
					"name":          "get",
					"class_context": "ReportView",
					"uid":           "content-entity:report-get",
					"line_number":   10,
					"end_line":      12,
					"lang":          "python",
				},
			},
			"django",
			[]any{
				map[string]any{"method": "GET", "path": "/reports/", "handler": "ReportView.get"},
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
	if got, want := payloadStr(intent.Payload, "framework"), "django"; got != want {
		t.Fatalf("framework = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "resolution_method"), codeprovenance.MethodSameFile; got != want {
		t.Fatalf("resolution_method = %q, want %q", got, want)
	}
}

func TestBuildHandlesRouteIntentRowsSkipsUnknownHandler(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"server.js",
			[]map[string]any{
				{"name": "getWidgets", "uid": "content-entity:gw", "line_number": 10, "end_line": 20},
			},
			"express",
			[]any{
				map[string]any{"method": "GET", "path": "/widgets", "handler": "doesNotExist"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no HANDLES_ROUTE intent for unknown handler, got %d", len(intents))
	}
}

func TestBuildHandlesRouteIntentRowsSkipsAmbiguousHandler(t *testing.T) {
	t.Parallel()

	// "handle" is defined twice across two files (ambiguous repo-wide) and is
	// not unique within the route file, so no edge may be produced.
	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"routes.js",
			nil,
			"express",
			[]any{
				map[string]any{"method": "GET", "path": "/ambiguous", "handler": "handle"},
			},
		),
		{
			FactKind: "file",
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"relative_path": "a.js",
				"parsed_file_data": map[string]any{
					"path": "a.js",
					"functions": []map[string]any{
						{"name": "handle", "uid": "content-entity:a", "line_number": 1, "end_line": 2},
					},
				},
			},
		},
		{
			FactKind: "file",
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"relative_path": "b.js",
				"parsed_file_data": map[string]any{
					"path": "b.js",
					"functions": []map[string]any{
						{"name": "handle", "uid": "content-entity:b", "line_number": 1, "end_line": 2},
					},
				},
			},
		},
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no HANDLES_ROUTE intent for ambiguous handler, got %d", len(intents))
	}
}

func TestBuildHandlesRouteIntentRowsSkipsEntryWithoutHandler(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"server.js",
			[]map[string]any{
				{"name": "getWidgets", "uid": "content-entity:gw", "line_number": 10, "end_line": 20},
			},
			"express",
			[]any{
				map[string]any{"method": "GET", "path": "/widgets"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no HANDLES_ROUTE intent when handler is absent, got %d", len(intents))
	}
}

func TestBuildHandlesRouteIntentRowsSkipsFrameworkWithoutRouteEntries(t *testing.T) {
	t.Parallel()

	// Older Next.js facts may carry endpoint metadata without exact route entries;
	// the emitter must tolerate that and skip rather than invent a handler edge.
	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		{
			FactKind: "file",
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"relative_path": "app/route.ts",
				"parsed_file_data": map[string]any{
					"path": "app/route.ts",
					"functions": []map[string]any{
						{"name": "GET", "uid": "content-entity:get", "line_number": 1, "end_line": 2},
					},
					"framework_semantics": map[string]any{
						"frameworks": []any{"nextjs"},
						"nextjs": map[string]any{
							"module_kind":    "route",
							"route_segments": []any{"widgets"},
						},
					},
				},
			},
		},
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no HANDLES_ROUTE intent for framework without route_entries, got %d", len(intents))
	}
}

func TestBuildHandlesRouteIntentRowsEmitsNextJSRouteHandlerEntries(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"src/app/api/accounts/[id]/route.ts",
			[]map[string]any{
				{"name": "GET", "uid": "content-entity:get", "line_number": 3, "end_line": 5},
				{"name": "POST", "uid": "content-entity:post", "line_number": 7, "end_line": 9},
			},
			"nextjs",
			[]any{
				map[string]any{"method": "GET", "path": "/api/accounts/[id]", "handler": "GET"},
				map[string]any{"method": "POST", "path": "/api/accounts/[id]", "handler": "POST"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 2 {
		t.Fatalf("expected 2 HANDLES_ROUTE intents, got %d", len(intents))
	}
	seen := map[string]bool{}
	for _, intent := range intents {
		if got, want := payloadStr(intent.Payload, "framework"), "nextjs"; got != want {
			t.Fatalf("framework = %q, want %q", got, want)
		}
		seen[payloadStr(intent.Payload, "http_method")+" "+payloadStr(intent.Payload, "function_entity_id")] = true
	}
	if !seen["GET content-entity:get"] || !seen["POST content-entity:post"] {
		t.Fatalf("nextjs HANDLES_ROUTE intents = %#v, want GET and POST bindings", intents)
	}
}
