// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func buildRunsInIntentsForTest(t *testing.T, envelopes []facts.Envelope) []SharedProjectionIntentRow {
	t.Helper()
	generationID := "gen-1"
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, generationID)
	index := buildCodeEntityIndex(envelopes)
	return buildRunsInIntentRows(
		envelopes,
		index,
		contextByRepoID,
		time.Unix(0, 0).UTC(),
		runsInEvidenceSource,
	)
}

func TestBuildRunsInIntentRowsBindsResolvedRouteHandler(t *testing.T) {
	t.Parallel()

	// A deployed repo with a resolved route handler binds that handler Function
	// to the workload(s) its repository DEFINES.
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

	intents := buildRunsInIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 RUNS_IN intent, got %d", len(intents))
	}

	intent := intents[0]
	if intent.ProjectionDomain != DomainRunsIn {
		t.Fatalf("projection domain = %q, want %q", intent.ProjectionDomain, DomainRunsIn)
	}
	if got, want := payloadStr(intent.Payload, "function_id"), "content-entity:gw"; got != want {
		t.Fatalf("function_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "repo_id"), "repo-1"; got != want {
		t.Fatalf("repo_id = %q, want %q", got, want)
	}
	// The code-call materialization stage never proves a repo defines exactly one
	// Workload (admission/correlation runs in a separate handler), so the honest,
	// correlation-truthful flag is always ambiguous=true: the edge is a candidate
	// member of the repo's workload set, never an asserted single binding.
	if ambiguous, ok := intent.Payload["ambiguous"].(bool); !ok || !ambiguous {
		t.Fatalf("ambiguous = %v (ok=%v), want true (conservative candidate binding)", intent.Payload["ambiguous"], ok)
	}
}

func TestBuildRunsInIntentRowsEmitsPHPLaravelAtJoinedRouteMatches(t *testing.T) {
	t.Parallel()

	for _, handler := range []string{"UserController@index"} {
		handler := handler
		t.Run(handler, func(t *testing.T) {
			t.Parallel()
			controller := handlesRouteFileEnvelope(
				"repo-1",
				"routes/web.php",
				[]map[string]any{{
					"name":          "index",
					"class_context": "UserController",
					"uid":           "content-entity:user-index",
					"line_number":   8,
					"end_line":      10,
					"lang":          "php",
				}},
				"laravel",
				[]any{map[string]any{
					"method":  "GET",
					"path":    "/users",
					"handler": handler,
				}},
			)
			envelopes := []facts.Envelope{
				handlesRouteRepoEnvelope("repo-1"),
				controller,
			}

			intents := buildRunsInIntentsForTest(t, envelopes)
			if len(intents) != 1 {
				t.Fatalf("expected exactly 1 RUNS_IN intent, got %d", len(intents))
			}
			if got, want := payloadStr(intents[0].Payload, "function_id"), "content-entity:user-index"; got != want {
				t.Fatalf("function_id = %q, want %q", got, want)
			}
		})
	}
}

func TestBuildRunsInIntentRowsDoesNotResolveLaravelControllerFQN(t *testing.T) {
	t.Parallel()

	controller := handlesRouteFileEnvelope(
		"repo-1",
		"src/UserController.php",
		[]map[string]any{{
			"name":          "index",
			"class_context": "UserController",
			"uid":           "content-entity:other-user-index",
			"line_number":   8,
			"end_line":      10,
			"lang":          "php",
		}},
		"",
		nil,
	)
	controller.Payload["parsed_file_data"].(map[string]any)["namespace"] = `App\Http\Controllers`

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		controller,
		handlesRouteFileEnvelope(
			"repo-1",
			"routes/web.php",
			nil,
			"laravel",
			[]any{map[string]any{
				"method":  "GET",
				"path":    "/users",
				"handler": `App\Http\Controllers\UserController@index`,
			}},
		),
	}

	if intents := buildRunsInIntentsForTest(t, envelopes); len(intents) != 0 {
		t.Fatalf("expected no RUNS_IN intent for an FQN without per-declaration namespace evidence, got %#v", intents)
	}
}

func TestBuildRunsInIntentRowsEmitsOnePerHandlerFunction(t *testing.T) {
	t.Parallel()

	// Two distinct route entries that resolve to the same handler Function must
	// collapse to exactly one RUNS_IN intent per (handler Function, repo): the
	// edge binds the symbol to its runtime, independent of how many routes it
	// serves.
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
				map[string]any{"method": "GET", "path": "/widgets/all", "handler": "getWidgets"},
			},
		),
	}

	intents := buildRunsInIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 RUNS_IN intent for a single handler Function, got %d", len(intents))
	}
	if got, want := payloadStr(intents[0].Payload, "function_id"), "content-entity:gw"; got != want {
		t.Fatalf("function_id = %q, want %q", got, want)
	}
}

func TestBuildRunsInIntentRowsSkipsUnresolvableHandler(t *testing.T) {
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

	intents := buildRunsInIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no RUNS_IN intent for an unresolvable handler, got %d", len(intents))
	}
}

func TestBuildRunsInIntentRowsSkipsAmbiguousHandler(t *testing.T) {
	t.Parallel()

	// "handle" is defined twice across two files (ambiguous repo-wide) and is not
	// unique within the route file, so no Function resolves and no RUNS_IN edge is
	// produced. A guessed entrypoint would corrupt the symbol→runtime binding.
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

	intents := buildRunsInIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no RUNS_IN intent for an ambiguous handler, got %d", len(intents))
	}
}

func TestBuildRunsInIntentRowsSkipsEntryWithoutHandler(t *testing.T) {
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

	intents := buildRunsInIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no RUNS_IN intent when the route entry has no handler, got %d", len(intents))
	}
}
