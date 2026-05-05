package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesPHPFreeFunctionReturnPropertyChainAliasedCallsUsingTypedPropertyInference(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.php")
	calleePath := filepath.Join(repoRoot, "logger.php")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-php",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-php",
				"relative_path": "app.php",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 3,
							"end_line":    6,
							"uid":         "content-entity:php-run",
						},
						map[string]any{
							"name":        "createFactory",
							"line_number": 8,
							"end_line":    10,
							"uid":         "content-entity:php-create-factory",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "$logger.info",
							"inferred_obj_type": "Logger",
							"line_number":       4,
							"lang":              "php",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-php",
				"relative_path": "logger.php",
				"parsed_file_data": map[string]any{
					"path": calleePath,
					"functions": []any{
						map[string]any{
							"name":          "info",
							"class_context": "Logger",
							"line_number":   1,
							"end_line":      2,
							"uid":           "content-entity:php-logger-info",
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
	if got, want := rows[0]["callee_entity_id"], "content-entity:php-logger-info"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}
