// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestMultiCloudObservedRowFromRowPopulatesAWSValueAttributes proves the
// provider-neutral observed decoder reuses the SAME cloudObservedValueAttributes
// helper as the AWS-specific decoder, so an AWS AMI observation carries the
// "ami" comparison key when routed through the multi-cloud path (#5453).
func TestMultiCloudObservedRowFromRowPopulatesAWSValueAttributes(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0"
	payload := []byte(`{
		"arn": "` + arn + `",
		"resource_type": "aws_ec2_instance",
		"attributes": {"ami_id": "ami-000000000000000a"}
	}`)

	row, ok := multiCloudObservedRowFromRow("aws:123456789012:us-east-1:ec2", facts.AWSResourceFactKind, arn, payload)
	if !ok {
		t.Fatalf("multiCloudObservedRowFromRow() ok = false, want true")
	}
	want := map[string]string{"ami": "ami-000000000000000a"}
	if !reflect.DeepEqual(row.resource.Attributes, want) {
		t.Fatalf("row.resource.Attributes = %#v, want %#v", row.resource.Attributes, want)
	}
}

// TestMultiCloudStateRowFromPayloadPopulatesAWSValueAttributes proves the
// provider-neutral state decoder reuses stateDeclaredValueAttributes.
func TestMultiCloudStateRowFromPayloadPopulatesAWSValueAttributes(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"address": "module.ecs.aws_instance.supply-chain-demo",
		"type": "aws_instance",
		"attributes": {
			"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
			"ami": "ami-0123456789abcdef0"
		}
	}`)

	row, ok := multiCloudStateRowFromPayload("state_snapshot:s3:hash", "module.ecs.aws_instance.supply-chain-demo", payload)
	if !ok {
		t.Fatalf("multiCloudStateRowFromPayload() ok = false, want true")
	}
	want := map[string]string{"ami": "ami-0123456789abcdef0"}
	if !reflect.DeepEqual(row.Attributes, want) {
		t.Fatalf("row.Attributes = %#v, want %#v", row.Attributes, want)
	}
}

// TestMultiCloudStateRowFromPayloadPopulatesECSContainerImages proves the
// provider-neutral state decoder also reuses the bounded ECS extraction path.
func TestMultiCloudStateRowFromPayloadPopulatesECSContainerImages(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"address": "module.ecs.aws_ecs_task_definition.supply-chain-demo",
		"type": "aws_ecs_task_definition",
		"attributes": {
			"arn": "arn:aws:ecs:us-east-1:123456789012:task-definition/supply-chain-demo:1",
			"container_definitions": "[{\"name\":\"app\",\"image\":\"repo/app:v1\"}]"
		}
	}`)

	row, ok := multiCloudStateRowFromPayload("state_snapshot:s3:hash", "module.ecs.aws_ecs_task_definition.supply-chain-demo", payload)
	if !ok {
		t.Fatalf("multiCloudStateRowFromPayload() ok = false, want true")
	}
	want := []string{"repo/app:v1"}
	if !reflect.DeepEqual(row.ContainerImages, want) {
		t.Fatalf("row.ContainerImages = %#v, want %#v", row.ContainerImages, want)
	}
}
