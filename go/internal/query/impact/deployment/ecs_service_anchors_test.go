// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import "testing"

func TestECSServicesFromCloudResourcesRequiresServiceIdentity(t *testing.T) {
	resources := []map[string]any{
		{"resource_type": "ecs.task", "arn": "arn:aws:ecs:us-east-1:000000000000:task/c/a"},
		{"resource_type": "ecs.service", "arn": "arn:aws:ecs:us-east-1:000000000000:service/c/api"},
		{"resource_type": "ecs.service", "arn": ""},
	}
	got := ECSServicesFromCloudResources(resources)
	if len(got) != 1 || got[0] != "arn:aws:ecs:us-east-1:000000000000:service/c/api" {
		t.Fatalf("ECS service anchors = %#v, want only the observed service ARN", got)
	}
}

func TestDeploymentTraceCloudResourcesPreservesECSServiceARN(t *testing.T) {
	arn := "arn:aws:ecs:us-east-1:000000000000:service/c/api"
	rows, err := DeploymentTraceCloudResourcesFromRows([]map[string]any{{
		"id": "cloud-1", "name": "api", "kind": "ecs.service",
		"resource_type": "ecs.service", "arn": arn,
		"observation": map[string]any{"confidence": 0.9},
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := ECSServicesFromCloudResources(rows); len(got) != 1 || got[0] != arn {
		t.Fatalf("service anchors after query shaping = %#v, want %q", got, arn)
	}
}
