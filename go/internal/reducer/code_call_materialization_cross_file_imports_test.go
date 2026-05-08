package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
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

func TestExtractCodeCallRowsResolvesParsedTypeScriptNamespaceImports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "handlers", "v3", "remoteid.ts")
	calleePath := filepath.Join(repoRoot, "server", "resources", "jwt.ts")
	writeReducerTestFile(t, callerPath, `import * as jwt from '../../resources/jwt';

export const post = async req => {
  const token = await jwt.encode(req.payload, req.query.expire);
  return { token };
};
`)
	writeReducerTestFile(t, calleePath, `export const encode = async (data, expire) => {
  return String(data) + ':' + String(expire);
};
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, []string{callerPath, calleePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(caller) error = %v, want nil", err)
	}
	calleePayload, err := engine.ParsePath(repoRoot, calleePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(callee) error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, callerPayload, "post", "content-entity:ts-post")
	assignReducerTestFunctionUID(t, calleePayload, "encode", "content-entity:ts-encode")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":     "repo-ts",
				"imports_map": importsMap,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-ts",
				"relative_path":    reducerTestRelativePath(t, repoRoot, callerPath),
				"parsed_file_data": callerPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-ts",
				"relative_path":    reducerTestRelativePath(t, repoRoot, calleePath),
				"parsed_file_data": calleePayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v; caller_functions=%#v; caller_imports=%#v; caller_calls=%#v; imports_map=%#v", len(rows), rows, callerPayload["functions"], callerPayload["imports"], callerPayload["function_calls"], importsMap)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:ts-encode"; got != want {
		t.Fatalf("callee_entity_id = %#v, want parsed encode entity id %#v; rows=%#v", got, want, rows)
	}
}

func assignReducerTestFunctionUID(t *testing.T, payload map[string]any, name string, uid string) {
	t.Helper()
	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("payload functions = %T, want []map[string]any", payload["functions"])
	}
	for i := range functions {
		if functions[i]["name"] == name {
			functions[i]["uid"] = uid
			payload["functions"] = functions
			return
		}
	}
	t.Fatalf("payload missing function %q in %#v", name, functions)
}

func writeReducerTestFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
	}
}

func reducerTestRelativePath(t *testing.T, root string, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("Rel(%q, %q) error = %v, want nil", root, path, err)
	}
	return rel
}
