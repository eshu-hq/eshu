package cloudformation

import "testing"

func TestIsTemplateDetectsSAMTransformList(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"Transform": []any{
			"AWS::Serverless-2016-10-31",
		},
		"Resources": map[string]any{
			"Example": map[string]any{
				"Type": "Custom::Widget",
			},
		},
	}

	if !IsTemplate(document) {
		t.Fatalf("IsTemplate() = false, want true")
	}
}

func TestParseDefaultsParameterTypeToString(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"AWSTemplateFormatVersion": "2010-09-09",
		"Parameters": map[string]any{
			"Environment": map[string]any{
				"Default": "dev",
			},
		},
	}

	result := Parse(document, "/test/stack.json", 1, "json")
	params := result.Params
	if len(params) != 1 {
		t.Fatalf("len(params) = %d, want 1", len(params))
	}

	if got, want := params[0]["name"], "Environment"; got != want {
		t.Fatalf("parameter name = %#v, want %#v", got, want)
	}
	if got, want := params[0]["param_type"], "String"; got != want {
		t.Fatalf("parameter param_type = %#v, want %#v", got, want)
	}
}

func TestParsePersistsFileFormat(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"AWSTemplateFormatVersion": "2010-09-09",
		"Parameters": map[string]any{
			"Environment": map[string]any{
				"AllowedValues": []any{"dev", "prod"},
			},
		},
		"Resources": map[string]any{
			"DataBucket": map[string]any{
				"Type": "AWS::S3::Bucket",
				"DependsOn": []any{
					"BootstrapBucket",
				},
			},
		},
		"Outputs": map[string]any{
			"BucketArn": map[string]any{
				"Export": map[string]any{
					"Name": "Stack-BucketArn",
				},
			},
		},
	}

	for _, tc := range []struct {
		name       string
		fileFormat string
	}{
		{name: "yaml", fileFormat: "yaml"},
		{name: "json", fileFormat: "json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := Parse(document, "/test/stack."+tc.name, 1, tc.fileFormat)

			if got, want := result.Params[0]["file_format"], tc.fileFormat; got != want {
				t.Fatalf("parameter file_format = %#v, want %#v", got, want)
			}
			if got, want := result.Resources[0]["file_format"], tc.fileFormat; got != want {
				t.Fatalf("resource file_format = %#v, want %#v", got, want)
			}
			if got, want := result.Outputs[0]["file_format"], tc.fileFormat; got != want {
				t.Fatalf("output file_format = %#v, want %#v", got, want)
			}
			if got, want := result.Imports, []map[string]any{}; len(got) != len(want) {
				t.Fatalf("imports len = %d, want %d", len(got), len(want))
			}
			if got, want := result.Exports[0]["file_format"], tc.fileFormat; got != want {
				t.Fatalf("export file_format = %#v, want %#v", got, want)
			}
		})
	}
}
