// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"reflect"
	"testing"
)

func TestSummarizeECSTasksEvidenceMatrix(t *testing.T) {
	digest := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	cases := []struct {
		name           string
		running        int
		tasks          []ECSTaskFact
		wantCount      *int
		wantReason     string
		wantContainer  string
		wantContainers []string
	}{
		{name: "matching single container", running: 1, tasks: []ECSTaskFact{{ARN: "task-a", Containers: []ECSContainerFact{{Name: "api", Image: "000000000000.dkr.ecr.us-east-1.amazonaws.com/api:latest", Digest: digest}}}}, wantCount: intPtr(1), wantContainer: "api", wantContainers: []string{"api"}},
		{name: "observed zero", running: 0, wantCount: intPtr(0)},
		{name: "multi container counts task once", running: 1, tasks: []ECSTaskFact{{ARN: "task-a", Containers: []ECSContainerFact{{Name: "sidecar", Image: "docker.io/sidecar:latest", Digest: "sha256:other"}, {Name: "api", Image: "000000000000.dkr.ecr.us-east-1.amazonaws.com/api:latest", Digest: digest}}}}, wantCount: intPtr(1), wantContainer: "api", wantContainers: []string{"api"}},
		{name: "two matching containers count once", running: 1, tasks: []ECSTaskFact{{ARN: "task-a", Containers: []ECSContainerFact{{Name: "worker", Image: "000000000000.dkr.ecr.us-east-1.amazonaws.com/api:latest", Digest: digest}, {Name: "api", Image: "000000000000.dkr.ecr.us-east-1.amazonaws.com/api:latest", Digest: digest}}}}, wantCount: intPtr(1), wantContainer: "api", wantContainers: []string{"api", "worker"}},
		{name: "outside ECR skipped", running: 1, tasks: []ECSTaskFact{{ARN: "task-a", Containers: []ECSContainerFact{{Name: "api", Image: "docker.io/api:latest", Digest: digest}}}}, wantReason: "non_ecr_image_skipped"},
		{name: "incomplete enumeration", running: 2, tasks: []ECSTaskFact{{ARN: "task-a", Containers: []ECSContainerFact{{Name: "api", Image: "000000000000.dkr.ecr.us-east-1.amazonaws.com/api:latest", Digest: digest}}}}, wantReason: "incomplete_task_evidence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SummarizeECSTasks(tc.running, tc.tasks, []string{digest})
			if !sameOptionalInt(got.Count, tc.wantCount) || got.Reason != tc.wantReason || got.MatchedContainer != tc.wantContainer || !reflect.DeepEqual(got.MatchedContainers, tc.wantContainers) {
				t.Fatalf("summary = %#v, want count=%v reason=%q container=%q", got, tc.wantCount, tc.wantReason, tc.wantContainer)
			}
		})
	}
}

func TestSummarizeECSTasksNonECRWithoutImageReference(t *testing.T) {
	got := SummarizeECSTasks(1, []ECSTaskFact{{ARN: "task-a", Containers: []ECSContainerFact{{Name: "api", Image: "docker.io/api:latest", Digest: "sha256:abcdef"}}}}, nil)
	if got.Count != nil || got.Reason != "non_ecr_image_skipped" {
		t.Fatalf("non-ECR task without scanner reference = %#v", got)
	}
}

func TestSummarizeECSTasksIncompleteWithoutImageReference(t *testing.T) {
	got := SummarizeECSTasks(1, nil, nil)
	if got.Count != nil || got.Reason != "incomplete_task_evidence" {
		t.Fatalf("missing running task = %#v", got)
	}
}

func intPtr(value int) *int { return &value }

func sameOptionalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func TestDeploymentFactSummarySeparatesConfigAndECSRuntimeCounts(t *testing.T) {
	ctx := map[string]any{
		"_ecs_running_task_count":  1,
		"_ecs_matching_container":  "api",
		"_ecs_matching_containers": []string{"api"},
	}
	instances := []map[string]any{{"instance_id": "configured-instance"}, {"instance_id": "configured-instance-2"}}
	summary := BuildDeploymentFactSummary(ctx, instances, nil, nil, nil, nil, nil, nil, nil, nil, "", false)
	if got := summary["instance_count"]; got != 2 {
		t.Fatalf("config instance_count = %v, want 2", got)
	}
	if got := summary["ecs_running_task_count"]; got != 1 {
		t.Fatalf("ECS running task count = %v, want 1", got)
	}
	if got := summary["ecs_running_task_count_source"]; got != "aws_ecs_describe_services_tasks" {
		t.Fatalf("ECS count source = %v, want aws_ecs_describe_services_tasks", got)
	}
	if got := summary["ecs_running_task_evidence_tier"]; got != "runtime_confirmed" {
		t.Fatalf("ECS evidence tier = %v, want runtime_confirmed", got)
	}
	if got := summary["ecs_matched_container"]; got != "api" {
		t.Fatalf("matched container = %v, want api", got)
	}
	if got := summary["ecs_matched_containers"]; !reflect.DeepEqual(got, []string{"api"}) {
		t.Fatalf("matched containers = %v, want api", got)
	}
	if got := summary["deployment_truth_tier"]; got == "runtime_confirmed" {
		t.Fatalf("count independently promoted overall deployment tier: %v", got)
	}
}
