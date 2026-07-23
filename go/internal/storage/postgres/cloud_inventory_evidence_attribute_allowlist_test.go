// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCloudInventoryRecordFromRowAWSRawLocatorsNotSurfaced proves that an
// aws_resource row whose attributes map carries only raw provider locators
// (uri, cluster_arn) does NOT surface them on the record: none of those keys
// are in the AWS image/version allowlist, so the filtered result is empty and
// Attributes stays nil. AWS attribute readback is bounded to the closed
// image/version allowlist (issue #5449); it is not a raw passthrough like
// GCP's typed-depth payload, so a raw provider locator can never reach the
// cloud inventory route (codex #4373 P1 stays fixed).
func TestCloudInventoryRecordFromRowAWSRawLocatorsNotSurfaced(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ecs:us-east-1:000000000000:cluster/demo"
	payload := []byte(`{
		"arn":"` + arn + `",
		"resource_type":"aws_ecs_cluster",
		"attributes":{
			"uri":"https://example.invalid/raw/locator",
			"cluster_arn":"` + arn + `"
		}
	}`)

	record, ok := cloudInventoryRecordFromRow(facts.AWSResourceFactKind, arn, payload)
	if !ok {
		t.Fatal("cloudInventoryRecordFromRow() ok = false, want true")
	}
	if record.Attributes != nil {
		t.Fatalf("AWS Attributes = %#v, want nil (no allowlisted keys present)", record.Attributes)
	}
}

// TestCloudInventoryRecordFromRowAWSECSTaskAllowlistFiltersRawKeys proves the
// AWS allowlist surfaces only task_definition_arn plus the containers array
// filtered to {image, image_digest}, and drops every raw provider locator on
// the same payload: cluster_arn, network_interfaces, desired_status, group,
// launch_type, started_at, and the container's name/runtime_id sub-keys.
func TestCloudInventoryRecordFromRowAWSECSTaskAllowlistFiltersRawKeys(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ecs:us-east-1:000000000000:task/demo-cluster/0000000000000000000000000000000a"
	payload := []byte(`{
		"arn":"` + arn + `",
		"resource_type":"aws_ecs_task",
		"attributes":{
			"cluster_arn":"arn:aws:ecs:us-east-1:000000000000:cluster/demo-cluster",
			"task_definition_arn":"arn:aws:ecs:us-east-1:000000000000:task-definition/demo:1",
			"desired_status":"RUNNING",
			"group":"service:demo",
			"launch_type":"FARGATE",
			"started_at":"2026-01-01T00:00:00Z",
			"network_interfaces":[{"network_interface_id":"eni-000000000000000aa","private_ipv4_address":"10.0.0.5"}],
			"containers":[
				{"image":"000000000000.dkr.ecr.us-east-1.amazonaws.com/demo:latest","image_digest":"sha256:0000000000000000000000000000000000000000000000000000000000aa","name":"demo","runtime_id":"0000000000000000000000000000000000000000000000000000000000bb"}
			]
		}
	}`)

	record, ok := cloudInventoryRecordFromRow(facts.AWSResourceFactKind, arn, payload)
	if !ok {
		t.Fatal("cloudInventoryRecordFromRow() ok = false, want true")
	}
	if record.Attributes == nil {
		t.Fatal("Attributes = nil, want non-nil (task_definition_arn and containers are allowlisted)")
	}
	if got, want := record.Attributes["task_definition_arn"], "arn:aws:ecs:us-east-1:000000000000:task-definition/demo:1"; got != want {
		t.Fatalf("task_definition_arn = %#v, want %q", got, want)
	}
	for _, dropped := range []string{"cluster_arn", "desired_status", "group", "launch_type", "started_at", "network_interfaces"} {
		if _, present := record.Attributes[dropped]; present {
			t.Fatalf("raw key %q must be dropped, got %#v", dropped, record.Attributes[dropped])
		}
	}
	containers, ok := record.Attributes["containers"].([]map[string]any)
	if !ok || len(containers) != 1 {
		t.Fatalf("containers = %#v, want one filtered container map", record.Attributes["containers"])
	}
	container := containers[0]
	if got, want := container["image"], "000000000000.dkr.ecr.us-east-1.amazonaws.com/demo:latest"; got != want {
		t.Fatalf("containers[0].image = %#v, want %q", got, want)
	}
	if got, want := container["image_digest"], "sha256:0000000000000000000000000000000000000000000000000000000000aa"; got != want {
		t.Fatalf("containers[0].image_digest = %#v, want %q", got, want)
	}
	for _, dropped := range []string{"name", "runtime_id"} {
		if _, present := container[dropped]; present {
			t.Fatalf("container sub-key %q must be dropped, got %#v", dropped, container[dropped])
		}
	}
}

// TestCloudInventoryRecordFromRowAWSLambdaAllowlistFiltersRawKeys proves the
// AWS allowlist surfaces image_uri, resolved_image_uri, code_sha256, and
// version from a Lambda function payload, and drops role_arn/kms_key_arn/
// environment/vpc_config, matching the issue #5449 closed allowlist.
func TestCloudInventoryRecordFromRowAWSLambdaAllowlistFiltersRawKeys(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:lambda:us-east-1:000000000000:function:demo"
	payload := []byte(`{
		"arn":"` + arn + `",
		"resource_type":"aws_lambda_function",
		"attributes":{
			"image_uri":"000000000000.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
			"resolved_image_uri":"000000000000.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:0000000000000000000000000000000000000000000000000000000000cc",
			"code_sha256":"0000000000000000000000000000000000000000000000000000000000cc",
			"version":"$LATEST",
			"role_arn":"arn:aws:iam::000000000000:role/demo-lambda-role",
			"kms_key_arn":"arn:aws:kms:us-east-1:000000000000:key/0000",
			"environment":{"FOO":"redacted"},
			"vpc_config":{"subnet_ids":["subnet-000000000000000aa"]}
		}
	}`)

	record, ok := cloudInventoryRecordFromRow(facts.AWSResourceFactKind, arn, payload)
	if !ok {
		t.Fatal("cloudInventoryRecordFromRow() ok = false, want true")
	}
	if record.Attributes == nil {
		t.Fatal("Attributes = nil, want non-nil")
	}
	wantScalars := map[string]string{
		"image_uri":          "000000000000.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
		"resolved_image_uri": "000000000000.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:0000000000000000000000000000000000000000000000000000000000cc",
		"code_sha256":        "0000000000000000000000000000000000000000000000000000000000cc",
		"version":            "$LATEST",
	}
	for key, want := range wantScalars {
		if got := record.Attributes[key]; got != want {
			t.Fatalf("Attributes[%q] = %#v, want %q", key, got, want)
		}
	}
	for _, dropped := range []string{"role_arn", "kms_key_arn", "environment", "vpc_config"} {
		if _, present := record.Attributes[dropped]; present {
			t.Fatalf("raw key %q must be dropped, got %#v", dropped, record.Attributes[dropped])
		}
	}
}

// TestCloudInventoryRecordFromRowAzureAttributesAlwaysDropped proves the
// azure_cloud_resource allowlist mechanism is wired but currently closed: the
// azure_cloud_resource fact carries no image/version keys today (image
// metadata lives in the separate azure_image_reference fact kind, which is
// not part of this admission mapping), so every raw Azure attribute key
// (arm_resource_id, subscription_id, resource_group, tenant_id, tags,
// extension) is dropped and Attributes stays nil. This guards against a future
// change silently widening the Azure allowlist without an explicit review.
func TestCloudInventoryRecordFromRowAzureAttributesAlwaysDropped(t *testing.T) {
	t.Parallel()

	armID := "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/demo/providers/Microsoft.Compute/virtualMachines/demo"
	payload := []byte(`{
		"arm_resource_id":"` + armID + `",
		"resource_type":"microsoft.compute/virtualmachines",
		"attributes":{
			"arm_resource_id":"` + armID + `",
			"subscription_id":"00000000-0000-0000-0000-000000000000",
			"resource_group":"demo",
			"tenant_id":"00000000-0000-0000-0000-000000000001",
			"tags":{"env":"prod"},
			"extension":{"schema_version":"1.0.0","data":{"vmSize":"Standard_D2s_v3"}}
		}
	}`)

	record, ok := cloudInventoryRecordFromRow(facts.AzureCloudResourceFactKind, armID, payload)
	if !ok {
		t.Fatal("cloudInventoryRecordFromRow() ok = false, want true")
	}
	if record.Attributes != nil {
		t.Fatalf("Azure Attributes = %#v, want nil (no image/version keys allowlisted yet)", record.Attributes)
	}
}
