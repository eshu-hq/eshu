package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesCrossFileJSRequireImports(t *testing.T) {
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
					"bar": {calleePath},
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
							"name":        "*",
							"alias":       "helpers",
							"source":      "./helpers",
							"import_type": "require",
							"lang":        "javascript",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "bar",
							"full_name":   "helpers.bar",
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
							"name":        "bar",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:js-bar",
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
	if got, want := rows[0]["callee_entity_id"], "content-entity:js-bar"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_file"], "helpers.js"; got != want {
		t.Fatalf("callee_file = %#v, want %#v", got, want)
	}
}
