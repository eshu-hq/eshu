package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesCrossFileJSImportedFunctions(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.js")
	calleePath := filepath.Join(repoRoot, "helpers.js")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-js",
				"imports_map": map[string][]string{
					"helper": {calleePath},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-js",
				"relative_path": "app.js",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 3,
							"end_line":    5,
							"uid":         "content-entity:js-run",
						},
					},
					"imports": []any{
						map[string]any{
							"name":   "helper",
							"source": "./helpers",
							"lang":   "javascript",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "helper",
							"full_name":   "helper",
							"line_number": 4,
							"lang":        "javascript",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-js",
				"relative_path": "helpers.js",
				"parsed_file_data": map[string]any{
					"path": calleePath,
					"functions": []any{
						map[string]any{
							"name":        "helper",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:js-helper",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["caller_entity_id"], "content-entity:js-run"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:js-helper"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_file"], "helpers.js"; got != want {
		t.Fatalf("callee_file = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesCrossFileJSAliasedImports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.js")
	calleePath := filepath.Join(repoRoot, "helpers.js")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-js",
				"imports_map": map[string][]string{
					"helper": {calleePath},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-js",
				"relative_path": "app.js",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 3,
							"end_line":    5,
							"uid":         "content-entity:js-run",
						},
					},
					"imports": []any{
						map[string]any{
							"name":   "helper",
							"alias":  "runTask",
							"source": "./helpers",
							"lang":   "javascript",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "runTask",
							"full_name":   "runTask",
							"line_number": 4,
							"lang":        "javascript",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-js",
				"relative_path": "helpers.js",
				"parsed_file_data": map[string]any{
					"path": calleePath,
					"functions": []any{
						map[string]any{
							"name":        "helper",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:js-helper",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:js-helper"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesCrossFileJSNamespaceImports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.js")
	calleePath := filepath.Join(repoRoot, "helpers.js")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-js",
				"imports_map": map[string][]string{
					"list": {calleePath},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-js",
				"relative_path": "app.js",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 3,
							"end_line":    5,
							"uid":         "content-entity:js-run",
						},
					},
					"imports": []any{
						map[string]any{
							"name":   "*",
							"alias":  "service",
							"source": "./helpers",
							"lang":   "javascript",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "list",
							"full_name":   "service.list",
							"line_number": 4,
							"lang":        "javascript",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-js",
				"relative_path": "helpers.js",
				"parsed_file_data": map[string]any{
					"path": calleePath,
					"functions": []any{
						map[string]any{
							"name":        "list",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:js-list",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:js-list"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesCrossFileTSXImportedComponents(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.tsx")
	calleePath := filepath.Join(repoRoot, "ToolbarButton.tsx")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-tsx",
				"imports_map": map[string][]string{
					"ToolbarButton": {calleePath},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-tsx",
				"relative_path": "app.tsx",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "render",
							"line_number": 3,
							"end_line":    5,
							"uid":         "content-entity:tsx-render",
						},
					},
					"imports": []any{
						map[string]any{
							"name":   "ToolbarButton",
							"source": "./ToolbarButton",
							"lang":   "tsx",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "ToolbarButton",
							"full_name":   "ToolbarButton",
							"call_kind":   "jsx_component",
							"line_number": 4,
							"lang":        "tsx",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-tsx",
				"relative_path": "ToolbarButton.tsx",
				"parsed_file_data": map[string]any{
					"path": calleePath,
					"functions": []any{
						map[string]any{
							"name":        "ToolbarButton",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:tsx-toolbar-button",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:tsx-toolbar-button"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}
