// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesCrossRepoSCIPEdgeBySymbol(t *testing.T) {
	t.Parallel()

	appPath := "/workspace/app/main.py"
	libPath := "/workspace/lib/client.py"
	scipSymbol := "scip-python python acme_lib/client.py Client#request()."
	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-app",
			"relative_path": "main.py",
			"parsed_file_data": map[string]any{
				"path": appPath,
				"functions": []any{
					map[string]any{"name": "handler", "line_number": 1, "end_line": 5, "uid": "uid:app:handler"},
				},
				"function_calls_scip": []any{
					map[string]any{
						"caller_file":   appPath,
						"caller_line":   1,
						"caller_symbol": "scip-python python service/main.py handler().",
						"callee_symbol": scipSymbol,
						"ref_line":      3,
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-lib",
			"relative_path": "client.py",
			"parsed_file_data": map[string]any{
				"path": libPath,
				"functions": []any{
					map[string]any{
						"name":        "request",
						"line_number": 8,
						"end_line":    11,
						"uid":         "uid:lib:request",
						"scip_symbol": scipSymbol,
					},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:lib:request"); got != codeprovenance.MethodSCIP {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodSCIP)
	}
	row := codeCallRowForCallee(t, rows, "uid:lib:request")
	if got, want := anyToString(row["repo_id"]), "repo-app"; got != want {
		t.Fatalf("repo_id = %q, want %q", got, want)
	}
}

func TestExtractCodeCallRowsResolvesCrossRepoPackageExportSymbol(t *testing.T) {
	t.Parallel()

	appPath := "/workspace/app/src/page.ts"
	libPath := "/workspace/ui/src/index.ts"
	exportSymbol := "package:npm://registry.npmjs.org/@acme/ui#renderButton"
	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-app",
			"relative_path": "src/page.ts",
			"parsed_file_data": map[string]any{
				"path": appPath,
				"functions": []any{
					map[string]any{"name": "page", "line_number": 1, "end_line": 4, "uid": "uid:app:page"},
				},
				"function_calls": []any{
					map[string]any{
						"name":                  "renderButton",
						"full_name":             "renderButton",
						"line_number":           2,
						"lang":                  "typescript",
						"package_export_symbol": exportSymbol,
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-ui",
			"relative_path": "src/index.ts",
			"parsed_file_data": map[string]any{
				"path": libPath,
				"functions": []any{
					map[string]any{
						"name":                  "renderButton",
						"line_number":           3,
						"end_line":              6,
						"uid":                   "uid:ui:renderButton",
						"package_export_symbol": exportSymbol,
					},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:ui:renderButton"); got != codeprovenance.MethodImportBinding {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
}

func TestExtractCodeCallRowsSkipsAmbiguousCrossRepoSymbol(t *testing.T) {
	t.Parallel()

	appPath := "/workspace/app/main.py"
	sharedSymbol := "scip-python python shared/client.py request()."
	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-app",
			"relative_path": "main.py",
			"parsed_file_data": map[string]any{
				"path": appPath,
				"functions": []any{
					map[string]any{"name": "handler", "line_number": 1, "end_line": 5, "uid": "uid:app:handler"},
				},
				"function_calls_scip": []any{
					map[string]any{
						"caller_file":   appPath,
						"caller_line":   1,
						"callee_symbol": sharedSymbol,
						"ref_line":      3,
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-lib-a",
			"relative_path": "client.py",
			"parsed_file_data": map[string]any{
				"path": "/workspace/lib-a/client.py",
				"functions": []any{
					map[string]any{"name": "request", "line_number": 1, "uid": "uid:lib-a:request", "scip_symbol": sharedSymbol},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-lib-b",
			"relative_path": "client.py",
			"parsed_file_data": map[string]any{
				"path": "/workspace/lib-b/client.py",
				"functions": []any{
					map[string]any{"name": "request", "line_number": 1, "uid": "uid:lib-b:request", "scip_symbol": sharedSymbol},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for ambiguous cross-repo symbol; rows=%#v", len(rows), rows)
	}
}

func TestCodeCallDefinitionSymbolKeysIgnoreGenerationFields(t *testing.T) {
	t.Parallel()

	first := map[string]any{
		"uid":           "uid:generation-a",
		"fact_id":       "fact:generation-a",
		"generation_id": "generation-a",
		"scip_symbol":   "scip-go gomod example.com/lib request().",
	}
	second := map[string]any{
		"uid":           "uid:generation-b",
		"fact_id":       "fact:generation-b",
		"generation_id": "generation-b",
		"scip_symbol":   "scip-go gomod example.com/lib request().",
	}

	firstKeys := codeCallDefinitionSymbolKeys(first)
	secondKeys := codeCallDefinitionSymbolKeys(second)
	if len(firstKeys) != 1 || len(secondKeys) != 1 {
		t.Fatalf("symbol key counts = %d/%d, want 1/1", len(firstKeys), len(secondKeys))
	}
	if firstKeys[0].key != secondKeys[0].key {
		t.Fatalf("symbol keys differ across generation fields: %q vs %q", firstKeys[0].key, secondKeys[0].key)
	}
	if firstKeys[0].method != codeprovenance.MethodSCIP {
		t.Fatalf("method = %q, want %q", firstKeys[0].method, codeprovenance.MethodSCIP)
	}
}

func codeCallRowForCallee(t *testing.T, rows []map[string]any, calleeID string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if anyToString(row["callee_entity_id"]) == calleeID {
			return row
		}
	}
	t.Fatalf("no code-call row for callee %q (rows=%d)", calleeID, len(rows))
	return nil
}
