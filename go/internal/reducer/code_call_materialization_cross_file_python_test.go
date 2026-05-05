package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesCrossFilePythonAliasedFromImports(
	t *testing.T,
) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.py")
	calleePath := filepath.Join(repoRoot, "lib", "factory.py")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-python",
				"imports_map": map[string][]string{
					"create_app": {calleePath},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "app.py",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 3,
							"end_line":    5,
							"uid":         "content-entity:python-run",
						},
					},
					"imports": []any{
						map[string]any{
							"name":   "create_app",
							"alias":  "make_app",
							"source": "lib.factory",
							"lang":   "python",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "make_app",
							"full_name":   "make_app",
							"line_number": 4,
							"lang":        "python",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "lib/factory.py",
				"parsed_file_data": map[string]any{
					"path": calleePath,
					"functions": []any{
						map[string]any{
							"name":        "create_app",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:python-create-app",
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
	if got, want := rows[0]["callee_entity_id"], "content-entity:python-create-app"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesCrossFilePythonModuleAliasCalls(
	t *testing.T,
) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.py")
	calleePath := filepath.Join(repoRoot, "pkg", "mod.py")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-python",
				"imports_map": map[string][]string{
					"run": {calleePath},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "app.py",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "main",
							"line_number": 3,
							"end_line":    5,
							"uid":         "content-entity:python-main",
						},
					},
					"imports": []any{
						map[string]any{
							"name":        "pkg.mod",
							"alias":       "mod",
							"source":      "pkg.mod",
							"lang":        "python",
							"import_type": "import",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "run",
							"full_name":   "mod.run",
							"line_number": 4,
							"lang":        "python",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "pkg/mod.py",
				"parsed_file_data": map[string]any{
					"path": calleePath,
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:python-run-target",
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
	if got, want := rows[0]["callee_entity_id"], "content-entity:python-run-target"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}
