package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestResolveGenericCalleeUsesJavaReceiverTypeBeforeRepoUniqueName(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/example/Worker.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/Worker.java",
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 1,
							"end_line":    5,
							"uid":         "content-entity:java-worker-run",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "process",
							"full_name":         "process",
							"inferred_obj_type": "Service",
							"argument_types":    []any{"Task"},
							"line_number":       3,
							"lang":              "java",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/example/Service.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/Service.java",
					"functions": []any{
						map[string]any{
							"name":            "process",
							"class_context":   "Service",
							"parameter_types": []any{"Task"},
							"line_number":     1,
							"end_line":        3,
							"uid":             "content-entity:java-service-process",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/example/Other.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/Other.java",
					"functions": []any{
						map[string]any{
							"name":            "process",
							"class_context":   "Other",
							"parameter_types": []any{"Task"},
							"line_number":     1,
							"end_line":        3,
							"uid":             "content-entity:java-other-process",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "content-entity:java-service-process"); got != codeprovenance.MethodTypeInferred {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodTypeInferred)
	}
	assertReducerNoCodeCallRow(t, rows, "content-entity:java-worker-run", "content-entity:java-other-process")
}
