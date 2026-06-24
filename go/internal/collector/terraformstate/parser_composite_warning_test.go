// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestParserSummarizesUnsupportedCompositeAttributeWarnings(t *testing.T) {
	t.Parallel()

	recorder := &stubCompositeCaptureRecorder{}
	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver(
		[2]string{"aws_s3_bucket", "acl"},
		[2]string{"aws_security_group", "ingress"},
	)
	options.CompositeCaptureMetrics = recorder

	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed",
			"type":"aws_s3_bucket",
			"name":"logs",
			"instances":[
				{"attributes":{
					"acl":"private",
					"server_side_encryption_configuration":[
						{"rule":[{"apply_server_side_encryption_by_default":[{"sse_algorithm":"AES256"}]}]}
					]
				}},
				{"attributes":{
					"acl":"private",
					"server_side_encryption_configuration":[
						{"rule":[{"apply_server_side_encryption_by_default":[{"sse_algorithm":"aws:kms"}]}]}
					]
				}},
				{"attributes":{
					"acl":"private",
					"server_side_encryption_configuration":[
						{"rule":[{"apply_server_side_encryption_by_default":[{"sse_algorithm":"AES256"}]}]}
					]
				}}
			]
		},{
			"mode":"managed",
			"type":"aws_security_group",
			"name":"web",
			"instances":[{
				"attributes":{
					"ingress":[{"from_port":443,"to_port":443}]
				}
			}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	for _, resource := range factsByKind(result.Facts, facts.TerraformStateResourceFactKind) {
		attributes, ok := resource.Payload["attributes"].(map[string]any)
		if !ok {
			t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
		}
		if resource.Payload["type"] == "aws_s3_bucket" {
			if _, present := attributes["server_side_encryption_configuration"]; present {
				t.Fatalf("unsupported composite should be absent, got %#v", attributes["server_side_encryption_configuration"])
			}
			if got, want := attributes["acl"], "private"; got != want {
				t.Fatalf("attributes[acl] = %#v, want %q", got, want)
			}
		}
		if resource.Payload["type"] == "aws_security_group" {
			if _, present := attributes["ingress"]; !present {
				t.Fatalf("supported composite ingress missing from attributes %#v", attributes)
			}
		}
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
	if got, want := warning.Payload["resource_type"], "aws_s3_bucket"; got != want {
		t.Fatalf("resource_type = %#v, want %#v", got, want)
	}
	if got, want := warning.Payload["attribute_key"], "server_side_encryption_configuration"; got != want {
		t.Fatalf("attribute_key = %#v, want %#v", got, want)
	}
	if got, want := warning.Payload["occurrence_count"], int64(3); got != want {
		t.Fatalf("occurrence_count = %#v, want %#v", got, want)
	}
	if got, want := result.WarningsByKind["unsupported_composite_attribute"], int64(1); got != want {
		t.Fatalf("WarningsByKind[unsupported_composite_attribute] = %d, want %d", got, want)
	}
	if got := atomic.LoadInt64(&recorder.calls); got != 3 {
		t.Fatalf("recorder.calls = %d, want 3 total skip observations", got)
	}
}
