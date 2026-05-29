package terraformstate_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/terraformschema"
)

// TestParserCapturesPackagedDataSourceCompositeAttribute is the #566
// end-to-end parser proof for data sources. aws_iam_policy_document.statement
// is declared in the packaged AWS data_source_schemas bundle, and Terraform
// state sends it through the same resource.Type + attributes path as managed
// resources. The parser must therefore capture it as evidence instead of
// dropping it as schema_unknown.
func TestParserCapturesPackagedDataSourceCompositeAttribute(t *testing.T) {
	t.Parallel()

	resolver, err := terraformstate.LoadPackagedSchemaResolver(terraformschema.DefaultSchemaDir())
	if err != nil {
		t.Fatalf("LoadPackagedSchemaResolver() error = %v, want nil", err)
	}
	if resolver == nil {
		t.Fatal("LoadPackagedSchemaResolver() = nil, want resolver loaded from packaged schemas")
	}

	recorder := &stubCompositeCaptureRecorder{}
	options := parseFixtureOptions(t)
	options.SchemaResolver = resolver
	options.CompositeCaptureMetrics = recorder

	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"data",
			"type":"aws_iam_policy_document",
			"name":"allow_logs",
			"instances":[{
				"attributes":{
					"statement":[
						{
							"actions":["s3:GetObject"],
							"effect":"Allow",
							"resources":["arn:aws:s3:::example/*"]
						}
					]
				}
			}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	resource := factByKind(t, result.Facts, facts.TerraformStateResourceFactKind)
	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}
	statement, ok := attributes["statement"].([]any)
	if !ok || len(statement) != 1 {
		t.Fatalf("attributes[statement] = %#v, want []any of length 1", attributes["statement"])
	}
	statementMap, ok := statement[0].(map[string]any)
	if !ok {
		t.Fatalf("statement[0] = %#v, want map[string]any", statement[0])
	}
	if got, want := statementMap["effect"], "Allow"; got != want {
		t.Fatalf("statement[0][effect] = %#v, want %q", got, want)
	}
	actions, ok := statementMap["actions"].([]any)
	if !ok || len(actions) != 1 || actions[0] != "s3:GetObject" {
		t.Fatalf("statement[0][actions] = %#v, want [s3:GetObject]", statementMap["actions"])
	}
	if got, want := atomic.LoadInt64(&recorder.calls), int64(0); got != want {
		t.Fatalf("composite skip calls = %d, want %d for schema-backed data-source composite", got, want)
	}
}

// TestParserKeepsUnsupportedPackagedCompositeFailClosed proves unsupported
// remote E2E shapes still take the existing warning/metric path. There is no
// packaged cloudinit provider schema today, so cloudinit_config.part must stay
// absent from emitted evidence and must report schema_unknown.
func TestParserKeepsUnsupportedPackagedCompositeFailClosed(t *testing.T) {
	t.Parallel()

	resolver, err := terraformstate.LoadPackagedSchemaResolver(terraformschema.DefaultSchemaDir())
	if err != nil {
		t.Fatalf("LoadPackagedSchemaResolver() error = %v, want nil", err)
	}
	if resolver == nil {
		t.Fatal("LoadPackagedSchemaResolver() = nil, want resolver loaded from packaged schemas")
	}

	recorder := &stubCompositeCaptureRecorder{}
	options := parseFixtureOptions(t)
	options.SchemaResolver = resolver
	options.CompositeCaptureMetrics = recorder

	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"data",
			"type":"cloudinit_config",
			"name":"rendered",
			"instances":[{
				"attributes":{
					"part":[{"content":"echo hello","content_type":"text/x-shellscript"}]
				}
			}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	resource := factByKind(t, result.Facts, facts.TerraformStateResourceFactKind)
	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}
	if _, present := attributes["part"]; present {
		t.Fatalf("attributes[part] should be absent for unsupported cloudinit composite, got %#v", attributes["part"])
	}
	if got, want := atomic.LoadInt64(&recorder.calls), int64(1); got != want {
		t.Fatalf("composite skip calls = %d, want %d", got, want)
	}
	if got, want := recorder.last.ResourceType, "cloudinit_config"; got != want {
		t.Fatalf("recorded ResourceType = %q, want %q", got, want)
	}
	if got, want := recorder.last.AttributeKey, "part"; got != want {
		t.Fatalf("recorded AttributeKey = %q, want %q", got, want)
	}
	if got, want := recorder.last.Reason, terraformstate.CompositeCaptureSkipReasonSchemaUnknown; got != want {
		t.Fatalf("recorded Reason = %q, want %q", got, want)
	}
}
