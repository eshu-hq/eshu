package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesPHPAnonymousClassReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "anonymous_class.php")

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
				"relative_path": "anonymous_class.php",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"classes": []any{
						map[string]any{
							"name":        "anonymous_class_4",
							"bases":       []any{"Logger"},
							"line_number": 4,
							"end_line":    7,
							"uid":         "content-entity:php-anonymous-class",
						},
					},
					"functions": []any{
						map[string]any{
							"name":          "run",
							"class_context": "Config",
							"line_number":   3,
							"end_line":      10,
							"uid":           "content-entity:php-run",
						},
						map[string]any{
							"name":          "info",
							"class_context": "anonymous_class_4",
							"line_number":   5,
							"end_line":      7,
							"uid":           "content-entity:php-anonymous-class-info",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "$logger.info",
							"inferred_obj_type": "anonymous_class_4",
							"line_number":       8,
							"lang":              "php",
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
	if got, want := rows[0]["callee_entity_id"], "content-entity:php-anonymous-class-info"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}
