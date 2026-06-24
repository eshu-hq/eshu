// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate_test

import (
	"context"
	"os"
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
	warning := factByKind(t, result.Facts, facts.TerraformStateWarningFactKind)
	if got, want := warning.Payload["warning_kind"], "unsupported_composite_attribute"; got != want {
		t.Fatalf("warning_kind = %#v, want %#v", got, want)
	}
	if got, want := warning.Payload["reason"], terraformstate.CompositeCaptureSkipReasonSchemaUnknown; got != want {
		t.Fatalf("reason = %#v, want %#v", got, want)
	}
	assertWarningClassification(t, warning, "warning", "provider_schema_support")
	if got, want := warning.Payload["resource_type"], "cloudinit_config"; got != want {
		t.Fatalf("resource_type = %#v, want %#v", got, want)
	}
	if got, want := warning.Payload["attribute_key"], "part"; got != want {
		t.Fatalf("attribute_key = %#v, want %#v", got, want)
	}
}

func TestParserClassifiesCloudinitPartFixtureAsUnsupportedComposite(t *testing.T) {
	t.Parallel()

	state, err := os.ReadFile("testdata/cloudinit_config_part.tfstate.json")
	if err != nil {
		t.Fatalf("read cloudinit_config.part fixture: %v", err)
	}

	resolver, err := terraformstate.LoadPackagedSchemaResolver(terraformschema.DefaultSchemaDir())
	if err != nil {
		t.Fatalf("LoadPackagedSchemaResolver() error = %v, want nil", err)
	}
	if resolver == nil {
		t.Fatal("LoadPackagedSchemaResolver() = nil, want resolver loaded from packaged schemas")
	}
	if resolver.HasAttribute("cloudinit_config", "part") {
		t.Fatal("HasAttribute(cloudinit_config, part) = true, want unsupported without packaged cloudinit schema")
	}

	recorder := &stubCompositeCaptureRecorder{}
	options := parseFixtureOptions(t)
	options.SchemaResolver = resolver
	options.CompositeCaptureMetrics = recorder

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(string(state)), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	cloudinitResources := 0
	for _, resource := range factsByKind(result.Facts, facts.TerraformStateResourceFactKind) {
		if got, want := resource.Payload["type"], "cloudinit_config"; got != want {
			continue
		}
		cloudinitResources++
		attributes, ok := resource.Payload["attributes"].(map[string]any)
		if !ok {
			t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
		}
		if _, present := attributes["part"]; present {
			t.Fatalf("attributes[part] should be absent for unsupported cloudinit composite, got %#v", attributes["part"])
		}
	}
	if got, want := cloudinitResources, 2; got != want {
		t.Fatalf("cloudinit resource facts = %d, want %d", got, want)
	}

	if got, want := atomic.LoadInt64(&recorder.calls), int64(2); got != want {
		t.Fatalf("composite skip calls = %d, want %d", got, want)
	}
	warnings := factsByKind(result.Facts, facts.TerraformStateWarningFactKind)
	if got, want := len(warnings), 1; got != want {
		t.Fatalf("warning fact count = %d, want %d: %#v", got, want, warnings)
	}
	warning := warnings[0]
	if got, want := warning.Payload["warning_kind"], "unsupported_composite_attribute"; got != want {
		t.Fatalf("warning_kind = %#v, want %#v", got, want)
	}
	if got, want := warning.Payload["reason"], terraformstate.CompositeCaptureSkipReasonSchemaUnknown; got != want {
		t.Fatalf("reason = %#v, want %#v", got, want)
	}
	assertWarningClassification(t, warning, "warning", "provider_schema_support")
	if got, want := warning.Payload["resource_type"], "cloudinit_config"; got != want {
		t.Fatalf("resource_type = %#v, want %#v", got, want)
	}
	if got, want := warning.Payload["attribute_key"], "part"; got != want {
		t.Fatalf("attribute_key = %#v, want %#v", got, want)
	}
	if got, want := warning.Payload["occurrence_count"], int64(2); got != want {
		t.Fatalf("occurrence_count = %#v, want %#v", got, want)
	}
	if got, want := result.WarningsByKind["unsupported_composite_attribute"], int64(1); got != want {
		t.Fatalf("WarningsByKind[unsupported_composite_attribute] = %d, want %d", got, want)
	}
}

// TestParserClassifiesSensitivePackagedCompositeAsIntentionalSkip proves
// schema-backed composites that are unsafe to persist are not reported as
// provider-schema gaps. Lambda environment blocks are declared by the AWS
// schema, but Eshu must drop them as intentionally unsupported sensitive state.
func TestParserClassifiesSensitivePackagedCompositeAsIntentionalSkip(t *testing.T) {
	t.Parallel()

	resolver, err := terraformstate.LoadPackagedSchemaResolver(terraformschema.DefaultSchemaDir())
	if err != nil {
		t.Fatalf("LoadPackagedSchemaResolver() error = %v, want nil", err)
	}
	if resolver == nil {
		t.Fatal("LoadPackagedSchemaResolver() = nil, want resolver loaded from packaged schemas")
	}
	if !resolver.HasAttribute("aws_lambda_function", "environment") {
		t.Fatal("HasAttribute(aws_lambda_function, environment) = false, want packaged AWS schema proof")
	}

	recorder := &stubCompositeCaptureRecorder{}
	options := parseFixtureOptions(t)
	options.SchemaResolver = resolver
	options.CompositeCaptureMetrics = recorder

	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed",
			"type":"aws_lambda_function",
			"name":"worker",
			"instances":[{
				"attributes":{
					"function_name":"worker",
					"environment":[{"variables":{"TOKEN":"plain-secret"}}]
				}
			}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	assertNoRawSecret(t, result.Facts, "plain-secret")
	resource := factByKind(t, result.Facts, facts.TerraformStateResourceFactKind)
	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}
	if _, present := attributes["environment"]; present {
		t.Fatalf("attributes[environment] should be absent for sensitive Lambda environment block, got %#v", attributes["environment"])
	}
	if got, want := atomic.LoadInt64(&recorder.calls), int64(1); got != want {
		t.Fatalf("composite skip calls = %d, want %d", got, want)
	}
	if got, want := recorder.last.Reason, terraformstate.CompositeCaptureSkipReasonSensitiveSource; got != want {
		t.Fatalf("recorded Reason = %q, want %q", got, want)
	}

	warning := factByKind(t, result.Facts, facts.TerraformStateWarningFactKind)
	if got, want := warning.Payload["warning_kind"], "composite_attribute_skipped"; got != want {
		t.Fatalf("warning_kind = %#v, want %#v", got, want)
	}
	if got, want := warning.Payload["reason"], terraformstate.CompositeCaptureSkipReasonSensitiveSource; got != want {
		t.Fatalf("reason = %#v, want %#v", got, want)
	}
	assertWarningClassification(t, warning, "info", "accepted_guardrail")
	if got, want := warning.Payload["resource_type"], "aws_lambda_function"; got != want {
		t.Fatalf("resource_type = %#v, want %#v", got, want)
	}
	if got, want := warning.Payload["attribute_key"], "environment"; got != want {
		t.Fatalf("attribute_key = %#v, want %#v", got, want)
	}
}
