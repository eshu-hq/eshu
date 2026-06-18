package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsBlocksTypeScriptDirectImportFallbackToRepoUnique(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "main.ts")
	importedPath := filepath.Join(repoRoot, "lib.ts")
	decoyPath := filepath.Join(repoRoot, "other.ts")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-ts-import-missing",
				"imports_map": map[string][]string{
					"helper":    {decoyPath},
					"notHelper": {importedPath},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-ts-import-missing",
				"relative_path": "main.ts",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "caller",
							"line_number": 3,
							"end_line":    5,
							"uid":         "content-entity:ts-caller",
						},
					},
					"imports": []any{
						map[string]any{
							"name":   "helper",
							"source": "./lib",
							"lang":   "typescript",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "helper",
							"full_name":   "helper",
							"line_number": 4,
							"lang":        "typescript",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-ts-import-missing",
				"relative_path": "lib.ts",
				"parsed_file_data": map[string]any{
					"path": importedPath,
					"functions": []any{
						map[string]any{
							"name":        "notHelper",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:ts-not-helper",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-ts-import-missing",
				"relative_path": "other.ts",
				"parsed_file_data": map[string]any{
					"path": decoyPath,
					"functions": []any{
						map[string]any{
							"name":        "helper",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:ts-decoy-helper",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for unresolved direct import before repo fallback: %#v", len(rows), rows)
	}
}
