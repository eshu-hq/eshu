package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesJavaOverloadWithTypedArguments(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-java",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "CyclonedxPluginAction.java",
				"parsed_file_data": map[string]any{
					"path": "CyclonedxPluginAction.java",
					"functions": []any{
						map[string]any{
							"name":            "configureBootJarTask",
							"class_context":   "CyclonedxPluginAction",
							"parameter_count": 2,
							"parameter_types": []any{"Project", "TaskProvider"},
							"line_number":     3,
							"end_line":        7,
							"uid":             "content-entity:java-configure-project",
						},
						map[string]any{
							"name":            "configureBootJarTask",
							"class_context":   "CyclonedxPluginAction",
							"parameter_count": 2,
							"parameter_types": []any{"BootJar", "TaskProvider"},
							"line_number":     9,
							"end_line":        11,
							"uid":             "content-entity:java-configure-boot-jar",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":           "configureBootJarTask",
							"full_name":      "configureBootJarTask",
							"class_context":  "CyclonedxPluginAction",
							"argument_count": 2,
							"argument_types": []any{"BootJar", "TaskProvider"},
							"line_number":    5,
							"lang":           "java",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v", len(rows), rows)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:java-configure-boot-jar"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}
