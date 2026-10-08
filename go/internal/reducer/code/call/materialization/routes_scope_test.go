// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialization

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// sameFileScopeRouteFileEnvelope builds a file fact envelope whose parsed
// file data carries functions and one framework's route entries, with an
// explicit checkout path so cross-repository same-named files stay distinct.
func sameFileScopeRouteFileEnvelope(
	repoID string,
	rawPath string,
	relativePath string,
	functions []map[string]any,
	framework string,
	routeEntries []any,
) facts.Envelope {
	functionItems := make([]any, 0, len(functions))
	for _, function := range functions {
		functionItems = append(functionItems, function)
	}
	return facts.Envelope{
		FactKind: "file",
		ScopeID:  "scope-1",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path":      rawPath,
				"functions": functionItems,
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

// TestBuildHandlesRouteIntentRowsHandlerStaysInsideTheRouteFile proves a
// route handler resolves only within the route file's own repository and
// path (#7642): a handler defined solely in a same-named file elsewhere
// leaves the route unresolved instead of binding across files.
func TestBuildHandlesRouteIntentRowsHandlerStaysInsideTheRouteFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		pathA string
		pathB string
	}{
		{name: "same bare name, different relative path", pathA: "lib/routes.js", pathB: "app/routes.js"},
		{name: "same relative path", pathA: "src/routes.js", pathB: "src/routes.js"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			envelopes := []facts.Envelope{
				handlesRouteRepoEnvelope("repo-a"),
				handlesRouteRepoEnvelope("repo-b"),
				sameFileScopeRouteFileEnvelope(
					"repo-a",
					"/repos/repo-a/"+tt.pathA,
					tt.pathA,
					[]map[string]any{
						{"name": "showWidget", "uid": "repo-a:showWidget", "line_number": 10, "end_line": 20},
					},
					"express",
					nil,
				),
				sameFileScopeRouteFileEnvelope(
					"repo-b",
					"/repos/repo-b/"+tt.pathB,
					tt.pathB,
					[]map[string]any{
						{"name": "unrelated", "uid": "repo-b:unrelated", "line_number": 1, "end_line": 5},
					},
					"express",
					[]any{
						map[string]any{"method": "GET", "path": "/widget", "handler": "showWidget"},
					},
				),
			}

			intents := buildHandlesRouteIntentsForTest(t, envelopes)

			for _, intent := range intents {
				if intent.Payload["function_entity_id"] == "repo-a:showWidget" {
					t.Fatalf("route handler bound across files: %v; same-file handler must stay in the route file", intent.Payload)
				}
			}
			if len(intents) != 0 {
				t.Fatalf("len(intents) = %d, want 0: %v", len(intents), intents)
			}
		})
	}
}

// TestBuildHandlesRouteIntentRowsKeepsOwnFileHandler is the positive
// control: a handler defined in the route file itself still resolves with
// resolution_method same_file.
func TestBuildHandlesRouteIntentRowsKeepsOwnFileHandler(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-a"),
		handlesRouteRepoEnvelope("repo-b"),
		sameFileScopeRouteFileEnvelope(
			"repo-a",
			"/repos/repo-a/lib/routes.js",
			"lib/routes.js",
			[]map[string]any{
				{"name": "showWidget", "uid": "repo-a:showWidget", "line_number": 10, "end_line": 20},
			},
			"express",
			nil,
		),
		sameFileScopeRouteFileEnvelope(
			"repo-b",
			"/repos/repo-b/app/routes.js",
			"app/routes.js",
			[]map[string]any{
				{"name": "showWidget", "uid": "repo-b:showWidget", "line_number": 10, "end_line": 20},
			},
			"express",
			[]any{
				map[string]any{"method": "GET", "path": "/widget", "handler": "showWidget"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)

	if len(intents) != 1 {
		t.Fatalf("len(intents) = %d, want 1: %v", len(intents), intents)
	}
	if got := intents[0].Payload["function_entity_id"]; got != "repo-b:showWidget" {
		t.Fatalf("function_entity_id = %v, want repo-b:showWidget", got)
	}
	if got := intents[0].Payload["resolution_method"]; got != codeprovenance.MethodSameFile {
		t.Fatalf("resolution_method = %v, want %v", got, codeprovenance.MethodSameFile)
	}
}
