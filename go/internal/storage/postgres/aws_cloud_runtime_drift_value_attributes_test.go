// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestAWSRuntimeResourceRowFromPayloadPopulatesAMIAttribute proves the
// cloud-side decoder normalizes the AWS-observed "ami_id" field onto the
// shared "ami" comparison key for aws_ec2_instance resources.
func TestAWSRuntimeResourceRowFromPayloadPopulatesAMIAttribute(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
		"resource_id": "i-0123456789abcdef0",
		"resource_type": "aws_ec2_instance",
		"attributes": {"ami_id": "ami-000000000000000a"}
	}`)

	row, ok := awsRuntimeResourceRowFromPayload("aws:123456789012:us-east-1:ec2", payload)
	if !ok {
		t.Fatalf("awsRuntimeResourceRowFromPayload() ok = false, want true")
	}
	want := map[string]string{"ami": "ami-000000000000000a"}
	if !reflect.DeepEqual(row.Attributes, want) {
		t.Fatalf("row.Attributes = %#v, want %#v", row.Attributes, want)
	}
}

// TestAWSRuntimeResourceRowFromPayloadPopulatesLambdaImageAndVersion proves
// the cloud-side decoder captures Lambda image_uri and version.
func TestAWSRuntimeResourceRowFromPayloadPopulatesLambdaImageAndVersion(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"arn": "arn:aws:lambda:us-east-1:123456789012:function:supply-chain-demo",
		"resource_id": "arn:aws:lambda:us-east-1:123456789012:function:supply-chain-demo",
		"resource_type": "lambda.function",
		"attributes": {
			"package_type": "Image",
			"image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest",
			"resolved_image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo@sha256:aa",
			"version": "$LATEST"
		}
	}`)

	row, ok := awsRuntimeResourceRowFromPayload("aws:123456789012:us-east-1:lambda", payload)
	if !ok {
		t.Fatalf("awsRuntimeResourceRowFromPayload() ok = false, want true")
	}
	want := map[string]string{
		"image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest",
		"version":   "$LATEST",
	}
	if !reflect.DeepEqual(row.Attributes, want) {
		t.Fatalf("row.Attributes = %#v, want %#v", row.Attributes, want)
	}
}

// TestAWSRuntimeResourceRowFromPayloadPopulatesECSContainerImages proves the
// cloud-side decoder extracts ONLY the bounded image list off the ECS
// task-definition containers attribute, never environment or secrets.
func TestAWSRuntimeResourceRowFromPayloadPopulatesECSContainerImages(t *testing.T) {
	t.Parallel()

	const secretMarker = "TOTALLY-SECRET-VALUE"
	payload := []byte(`{
		"arn": "arn:aws:ecs:us-east-1:123456789012:task-definition/supply-chain-demo:1",
		"resource_id": "arn:aws:ecs:us-east-1:123456789012:task-definition/supply-chain-demo:1",
		"resource_type": "ecs.task_definition",
		"attributes": {
			"containers": [
				{
					"image": "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest",
					"name": "supply-chain-demo",
					"essential": true,
					"environment": [{"name": "DATABASE_PASSWORD", "value": "` + secretMarker + `"}],
					"secrets": [{"name": "API_KEY", "value_from": "` + secretMarker + `"}]
				}
			],
			"cpu": "256",
			"memory": "512"
		}
	}`)

	row, ok := awsRuntimeResourceRowFromPayload("aws:123456789012:us-east-1:ecs", payload)
	if !ok {
		t.Fatalf("awsRuntimeResourceRowFromPayload() ok = false, want true")
	}
	want := []string{"123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest"}
	if !reflect.DeepEqual(row.ContainerImages, want) {
		t.Fatalf("row.ContainerImages = %#v, want %#v", row.ContainerImages, want)
	}
	if row.Attributes != nil {
		t.Fatalf("row.Attributes = %#v, want nil for ecs.task_definition (image comparison uses ContainerImages)", row.Attributes)
	}
	if strings.Contains(fmt.Sprintf("%#v", *row), secretMarker) {
		t.Fatalf("awsRuntimeResourceRowFromPayload() leaked a secret/environment value into the decoded ResourceRow")
	}
}

// TestAWSRuntimeResourceRowFromPayloadPopulatesLambdaFromProductionResourceType
// is the #5453 codex/owner P0 regression: the live AWS collector emits
// resource_type "aws_lambda_function" (constants_lambda.go), not the cassette's
// short-name "lambda.function". The observed decoder must accept the production
// string too, or Lambda image/version drift never fires outside the fixtures.
func TestAWSRuntimeResourceRowFromPayloadPopulatesLambdaFromProductionResourceType(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"arn": "arn:aws:lambda:us-east-1:123456789012:function:supply-chain-demo",
		"resource_id": "arn:aws:lambda:us-east-1:123456789012:function:supply-chain-demo",
		"resource_type": "aws_lambda_function",
		"attributes": {
			"package_type": "Image",
			"image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest",
			"version": "$LATEST"
		}
	}`)

	row, ok := awsRuntimeResourceRowFromPayload("aws:123456789012:us-east-1:lambda", payload)
	if !ok {
		t.Fatalf("awsRuntimeResourceRowFromPayload() ok = false, want true")
	}
	want := map[string]string{
		"image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest",
		"version":   "$LATEST",
	}
	if !reflect.DeepEqual(row.Attributes, want) {
		t.Fatalf("row.Attributes = %#v, want %#v (production aws_lambda_function must decode)", row.Attributes, want)
	}
}

// TestAWSRuntimeResourceRowFromPayloadPopulatesECSFromProductionResourceType is
// the ECS half of the same #5453 P0: the live collector emits
// "aws_ecs_task_definition" (constants_ecs.go), not "ecs.task_definition".
func TestAWSRuntimeResourceRowFromPayloadPopulatesECSFromProductionResourceType(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"arn": "arn:aws:ecs:us-east-1:123456789012:task-definition/supply-chain-demo:1",
		"resource_id": "arn:aws:ecs:us-east-1:123456789012:task-definition/supply-chain-demo:1",
		"resource_type": "aws_ecs_task_definition",
		"attributes": {
			"containers": [
				{"image": "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest", "name": "app"}
			]
		}
	}`)

	row, ok := awsRuntimeResourceRowFromPayload("aws:123456789012:us-east-1:ecs", payload)
	if !ok {
		t.Fatalf("awsRuntimeResourceRowFromPayload() ok = false, want true")
	}
	want := []string{"123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest"}
	if !reflect.DeepEqual(row.ContainerImages, want) {
		t.Fatalf("row.ContainerImages = %#v, want %#v (production aws_ecs_task_definition must decode)", row.ContainerImages, want)
	}
}

// TestAWSRuntimeStateRowFromPayloadPopulatesDeclaredAMI proves the
// state-side decoder captures the Terraform-declared "ami" attribute.
func TestAWSRuntimeStateRowFromPayloadPopulatesDeclaredAMI(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"address": "module.ecs.aws_instance.supply-chain-demo",
		"type": "aws_instance",
		"attributes": {
			"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
			"instance_type": "t3.micro",
			"ami": "ami-0123456789abcdef0"
		}
	}`)

	row, ok := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "module.ecs.aws_instance.supply-chain-demo", payload)
	if !ok {
		t.Fatalf("awsRuntimeStateRowFromPayload() ok = false, want true")
	}
	want := map[string]string{"ami": "ami-0123456789abcdef0"}
	if !reflect.DeepEqual(row.Attributes, want) {
		t.Fatalf("row.Attributes = %#v, want %#v", row.Attributes, want)
	}
}

// TestAWSRuntimeStateRowFromPayloadPopulatesDeclaredLambdaImageAndVersion
// proves the state-side decoder captures Terraform's declared image_uri and
// version attributes for aws_lambda_function.
func TestAWSRuntimeStateRowFromPayloadPopulatesDeclaredLambdaImageAndVersion(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"address": "aws_lambda_function.supply-chain-demo",
		"type": "aws_lambda_function",
		"attributes": {
			"arn": "arn:aws:lambda:us-east-1:123456789012:function:supply-chain-demo",
			"image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:v1",
			"version": "$LATEST"
		}
	}`)

	row, ok := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "aws_lambda_function.supply-chain-demo", payload)
	if !ok {
		t.Fatalf("awsRuntimeStateRowFromPayload() ok = false, want true")
	}
	want := map[string]string{
		"image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:v1",
		"version":   "$LATEST",
	}
	if !reflect.DeepEqual(row.Attributes, want) {
		t.Fatalf("row.Attributes = %#v, want %#v", row.Attributes, want)
	}
}

// TestAWSRuntimeStateRowFromPayloadPopulatesDeclaredECSContainerImages is
// the mandatory security proof for the state-side decoder: container_definitions
// is a JSON-encoded STRING that can carry environment variables and secret
// ARNs; the decoder must extract ONLY the bounded image list.
func TestAWSRuntimeStateRowFromPayloadPopulatesDeclaredECSContainerImages(t *testing.T) {
	t.Parallel()

	const secretMarker = "TOTALLY-SECRET-VALUE"
	containerDefinitions := `[{"name":"app","image":"123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:old",` +
		`"environment":[{"name":"SECRET","value":"` + secretMarker + `"}],` +
		`"secrets":[{"name":"API_KEY","valueFrom":"arn:aws:secretsmanager:us-east-1:123456789012:secret:x"}]}]`

	payload := []byte(`{
		"address": "module.ecs.aws_ecs_task_definition.supply-chain-demo",
		"type": "aws_ecs_task_definition",
		"attributes": {
			"arn": "arn:aws:ecs:us-east-1:123456789012:task-definition/supply-chain-demo:1",
			"family": "supply-chain-demo",
			"revision": 1,
			"container_definitions": ` + mustJSONString(containerDefinitions) + `
		}
	}`)

	row, ok := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "module.ecs.aws_ecs_task_definition.supply-chain-demo", payload)
	if !ok {
		t.Fatalf("awsRuntimeStateRowFromPayload() ok = false, want true")
	}
	want := []string{"123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:old"}
	if !reflect.DeepEqual(row.ContainerImages, want) {
		t.Fatalf("row.ContainerImages = %#v, want %#v", row.ContainerImages, want)
	}
	if row.Attributes != nil {
		t.Fatalf("row.Attributes = %#v, want nil for aws_ecs_task_definition", row.Attributes)
	}
	rendered := fmt.Sprintf("%#v", *row)
	if strings.Contains(rendered, secretMarker) {
		t.Fatalf("awsRuntimeStateRowFromPayload() leaked a secret/environment value into the decoded ResourceRow")
	}
	if strings.Contains(rendered, "secretsmanager") {
		t.Fatalf("awsRuntimeStateRowFromPayload() leaked a secret ARN reference into the decoded ResourceRow")
	}
}

func mustJSONString(s string) string {
	encoded, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
