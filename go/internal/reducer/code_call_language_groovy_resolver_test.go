package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCodeCallResolutionMethodGroovyClassQualifiedCall(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-groovy",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-groovy",
				"relative_path": "vars/deployPipeline.groovy",
				"parsed_file_data": map[string]any{
					"path": "vars/deployPipeline.groovy",
					"functions": []any{
						map[string]any{
							"name":        "call",
							"line_number": 2,
							"end_line":    4,
							"uid":         "content-entity:groovy-call",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "deployApp",
							"full_name":         "DeployHelper.deployApp",
							"inferred_obj_type": "DeployHelper",
							"line_number":       3,
							"lang":              "groovy",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-groovy",
				"relative_path": "src/org/example/DeployHelper.groovy",
				"parsed_file_data": map[string]any{
					"path": "src/org/example/DeployHelper.groovy",
					"functions": []any{
						map[string]any{
							"name":          "deployApp",
							"class_context": "DeployHelper",
							"line_number":   2,
							"end_line":      4,
							"uid":           "content-entity:groovy-deploy-app",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-groovy",
				"relative_path": "src/org/example/OtherHelper.groovy",
				"parsed_file_data": map[string]any{
					"path": "src/org/example/OtherHelper.groovy",
					"functions": []any{
						map[string]any{
							"name":          "deployApp",
							"class_context": "OtherHelper",
							"line_number":   2,
							"end_line":      4,
							"uid":           "content-entity:groovy-other-deploy-app",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "content-entity:groovy-deploy-app"); got != codeprovenance.MethodTypeInferred {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodTypeInferred)
	}
	assertReducerNoCodeCallRow(t, rows, "content-entity:groovy-call", "content-entity:groovy-other-deploy-app")
}

func TestCodeCallGroovyMissingClassQualifiedReceiverBlocksRepoUniqueFallback(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-groovy",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-groovy",
				"relative_path": "vars/deployPipeline.groovy",
				"parsed_file_data": map[string]any{
					"path": "vars/deployPipeline.groovy",
					"functions": []any{
						map[string]any{
							"name":        "call",
							"line_number": 2,
							"end_line":    4,
							"uid":         "content-entity:groovy-call",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "deployApp",
							"full_name":         "MissingHelper.deployApp",
							"inferred_obj_type": "MissingHelper",
							"line_number":       3,
							"lang":              "groovy",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-groovy",
				"relative_path": "src/org/example/DeployHelper.groovy",
				"parsed_file_data": map[string]any{
					"path": "src/org/example/DeployHelper.groovy",
					"functions": []any{
						map[string]any{
							"name":          "deployApp",
							"class_context": "DeployHelper",
							"line_number":   2,
							"end_line":      4,
							"uid":           "content-entity:groovy-deploy-app",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertReducerNoCodeCallRow(t, rows, "content-entity:groovy-call", "content-entity:groovy-deploy-app")
}
