package cloudformation

import "testing"

func TestParseCapturesConditionsAndNestedStackMetadata(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"AWSTemplateFormatVersion": "2010-09-09",
		"Conditions": map[string]any{
			"CreateNested": map[string]any{
				"Fn::Equals": []any{"prod", "prod"},
			},
		},
		"Resources": map[string]any{
			"NestedStack": map[string]any{
				"Type":      "AWS::CloudFormation::Stack",
				"Condition": "CreateNested",
				"Properties": map[string]any{
					"TemplateURL": "https://example.com/nested-stack.yaml",
				},
			},
		},
	}

	result := Parse(document, "/test/stack.yaml", 1, "yaml")

	if len(result.Conditions) != 1 {
		t.Fatalf("len(conditions) = %d, want 1", len(result.Conditions))
	}
	if got, want := result.Conditions[0]["name"], "CreateNested"; got != want {
		t.Fatalf("condition name = %#v, want %#v", got, want)
	}
	if got, want := result.Conditions[0]["expression"], "map[Fn::Equals:[prod prod]]"; got != want {
		t.Fatalf("condition expression = %#v, want %#v", got, want)
	}
	if got, want := result.Resources[0]["template_url"], "https://example.com/nested-stack.yaml"; got != want {
		t.Fatalf("resource template_url = %#v, want %#v", got, want)
	}
	if got, want := result.Resources[0]["condition"], "CreateNested"; got != want {
		t.Fatalf("resource condition = %#v, want %#v", got, want)
	}
}

func TestParseEvaluatesResolvableConditions(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"AWSTemplateFormatVersion": "2010-09-09",
		"Parameters": map[string]any{
			"Env": map[string]any{
				"Type":    "String",
				"Default": "prod",
			},
		},
		"Conditions": map[string]any{
			"CreateNested": map[string]any{
				"Fn::Equals": []any{
					map[string]any{"Ref": "Env"},
					"prod",
				},
			},
			"SkipNested": map[string]any{
				"Fn::Equals": []any{
					map[string]any{"Ref": "Env"},
					"dev",
				},
			},
		},
		"Resources": map[string]any{
			"NestedStack": map[string]any{
				"Type":      "AWS::CloudFormation::Stack",
				"Condition": "CreateNested",
				"Properties": map[string]any{
					"TemplateURL": "nested/network.yaml",
				},
			},
		},
	}

	result := Parse(document, "/test/stack.yaml", 1, "yaml")
	if len(result.Conditions) != 2 {
		t.Fatalf("len(conditions) = %d, want 2", len(result.Conditions))
	}

	if got, want := result.Conditions[0]["evaluated"], true; got != want {
		t.Fatalf("conditions[0][evaluated] = %#v, want %#v", got, want)
	}
	if got, want := result.Conditions[0]["evaluated_value"], true; got != want {
		t.Fatalf("conditions[0][evaluated_value] = %#v, want %#v", got, want)
	}
	if got, want := result.Conditions[1]["evaluated_value"], false; got != want {
		t.Fatalf("conditions[1][evaluated_value] = %#v, want %#v", got, want)
	}
	if got, want := result.Resources[0]["condition_evaluated"], true; got != want {
		t.Fatalf("resource condition_evaluated = %#v, want %#v", got, want)
	}
	if got, want := result.Resources[0]["condition_value"], true; got != want {
		t.Fatalf("resource condition_value = %#v, want %#v", got, want)
	}
}
