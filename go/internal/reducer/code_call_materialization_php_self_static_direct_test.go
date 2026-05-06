package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesPHPDirectSelfAndStaticReceiverCallsUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.php")
	configPath := filepath.Join(repoRoot, "config.php")

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
							"name":          "run",
							"class_context": "Config",
							"line_number":   3,
							"end_line":      6,
							"uid":           "content-entity:php-config-run",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "emit",
							"full_name":         "Config.emit",
							"inferred_obj_type": "Config",
							"line_number":       4,
							"lang":              "php",
						},
						map[string]any{
							"name":              "emit",
							"full_name":         "Config.emit",
							"inferred_obj_type": "Config",
							"line_number":       5,
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
				"relative_path": "config.php",
				"parsed_file_data": map[string]any{
					"path": configPath,
					"functions": []any{
						map[string]any{
							"name":          "emit",
							"class_context": "Config",
							"line_number":   1,
							"end_line":      2,
							"uid":           "content-entity:php-config-emit",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}

	for _, row := range rows {
		if got, want := row["callee_entity_id"], "content-entity:php-config-emit"; got != want {
			t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
		}
	}
}
