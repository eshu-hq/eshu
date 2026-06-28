// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudformation

import "testing"

func TestIsTemplateDetectsSAMResourceTypeWithoutTransform(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"Resources": map[string]any{
			"Function": map[string]any{
				"Type": "AWS::Serverless::Function",
			},
		},
	}

	if !IsTemplate(document) {
		t.Fatal("IsTemplate() = false, want true for SAM resource type")
	}
}

func TestParseCollectsCrossStackImports(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"AWSTemplateFormatVersion": "2010-09-09",
		"Resources": map[string]any{
			"Queue": map[string]any{
				"Type": "AWS::SQS::Queue",
				"Properties": map[string]any{
					"QueueName": map[string]any{
						"Fn::ImportValue": "SharedQueueName",
					},
				},
			},
			"Topic": map[string]any{
				"Type": "AWS::SNS::Topic",
				"Properties": map[string]any{
					"TopicName": map[string]any{
						"Fn::ImportValue": map[string]any{
							"Fn::Sub": "${NetworkStack}-TopicName",
						},
					},
				},
			},
		},
	}

	result := Parse(document, "/test/stack.yaml", 1, "yaml")
	if len(result.Imports) != 2 {
		t.Fatalf("len(imports) = %d, want 2: %#v", len(result.Imports), result.Imports)
	}
	if got, want := result.Imports[0]["name"], "SharedQueueName"; got != want {
		t.Fatalf("imports[0].name = %#v, want %#v", got, want)
	}
	if got, want := result.Imports[1]["name"], "map[Fn::Sub:${NetworkStack}-TopicName]"; got != want {
		t.Fatalf("imports[1].name = %#v, want %#v", got, want)
	}
}
