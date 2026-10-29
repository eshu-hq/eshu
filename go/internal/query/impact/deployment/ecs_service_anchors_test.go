// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestECSServicesFromCloudResourcesRequiresServiceIdentity(t *testing.T) {
	resources := []map[string]any{
		{"resource_type": "aws_ecs_task", "arn": "arn:aws:ecs:us-east-1:000000000000:task/c/a"},
		{"resource_type": "aws_ecs_service", "arn": "arn:aws:ecs:us-east-1:000000000000:service/c/api"},
		{"resource_type": "aws_ecs_service", "arn": ""},
	}
	got := ECSServicesFromCloudResources(resources)
	if len(got) != 1 || got[0] != "arn:aws:ecs:us-east-1:000000000000:service/c/api" {
		t.Fatalf("ECS service anchors = %#v, want only the observed service ARN", got)
	}
}

func TestECSServicesFromCloudResourcesAdmitesGovCloudAndChinaPartitions(t *testing.T) {
	resources := []map[string]any{
		{"resource_type": "aws_ecs_service", "arn": "arn:aws-us-gov:ecs:us-gov-west-1:123456789012:service/prod/api"},
		{"resource_type": "aws_ecs_service", "arn": "arn:aws-cn:ecs:cn-north-1:123456789012:service/prod/api"},
		{"resource_type": "aws_ecs_service", "arn": "arn:aws-iso:ecs:us-isob-east-1:123456789012:service/prod/api"},
	}
	got := ECSServicesFromCloudResources(resources)
	want := []string{"arn:aws-cn:ecs:cn-north-1:123456789012:service/prod/api", "arn:aws-us-gov:ecs:us-gov-west-1:123456789012:service/prod/api"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ECS service anchors = %#v, want %#v", got, want)
	}
}

func TestSelectDeclaredECSServiceARNAdmitesGovCloudPartition(t *testing.T) {
	const cluster = "arn:aws-us-gov:ecs:us-gov-west-1:123456789012:cluster/prod"
	const want = "arn:aws-us-gov:ecs:us-gov-west-1:123456789012:service/prod/api"
	entity := querycontract.EntityContent{RepoID: "repo-api", EntityType: "TerraformResource", EntityName: "aws_ecs_service.api", Metadata: map[string]any{
		"resource_type": "aws_ecs_service", "attributes": map[string]any{"name": "api", "cluster": cluster},
	}}
	got, reason := SelectDeclaredECSServiceARN("api", "repo-api", 1, []querycontract.EntityContent{entity})
	if got != want || reason != "" {
		t.Fatalf("got ARN=%q reason=%q, want ARN=%q reason=%q", got, reason, want, "")
	}
	bad := querycontract.EntityContent{RepoID: "repo-api", EntityType: "TerraformResource", EntityName: "aws_ecs_service.api", Metadata: map[string]any{
		"resource_type": "aws_ecs_service", "attributes": map[string]any{"name": "api", "cluster": "arn:aws-iso:ecs:us-isob-east-1:123456789012:cluster/prod"},
	}}
	if got, reason := SelectDeclaredECSServiceARN("api", "repo-api", 1, []querycontract.EntityContent{bad}); got != "" || reason != "ecs_declaration_unresolved" {
		t.Fatalf("unknown partition: got ARN=%q reason=%q, want unresolved", got, reason)
	}
}

func TestDeploymentTraceCloudResourcesPreservesECSServiceARN(t *testing.T) {
	arn := "arn:aws:ecs:us-east-1:000000000000:service/c/api"
	rows, err := DeploymentTraceCloudResourcesFromRows([]map[string]any{{
		"id": "cloud-1", "name": "api", "kind": "aws_ecs_service",
		"resource_type": "aws_ecs_service", "arn": arn,
		"observation": map[string]any{"confidence": 0.9},
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := ECSServicesFromCloudResources(rows); len(got) != 1 || got[0] != arn {
		t.Fatalf("service anchors after query shaping = %#v, want %q", got, arn)
	}
}

func TestSelectDeclaredECSServiceARNRequiresWorkloadSpecificLiteralIdentity(t *testing.T) {
	const cluster = "arn:aws:ecs:us-east-1:123456789012:cluster/prod"
	const want = "arn:aws:ecs:us-east-1:123456789012:service/prod/api"
	valid := querycontract.EntityContent{RepoID: "repo-api", EntityType: "TerraformResource", EntityName: "aws_ecs_service.api", Metadata: map[string]any{
		"resource_type": "aws_ecs_service", "attributes": map[string]any{"name": "api", "cluster": cluster},
	}}
	for _, tc := range []struct {
		name       string
		workloads  int
		rows       []querycontract.EntityContent
		wantARN    string
		wantReason string
	}{
		{name: "literal match", workloads: 1, rows: []querycontract.EntityContent{valid}, wantARN: want},
		{name: "shared repo", workloads: 2, rows: []querycontract.EntityContent{valid}, wantReason: "ecs_declaration_workload_ambiguous"},
		{name: "wrong workload", workloads: 1, rows: []querycontract.EntityContent{valid}, wantReason: "ecs_declaration_missing"},
		{name: "duplicate declaration", workloads: 1, rows: []querycontract.EntityContent{valid, valid}, wantReason: "ambiguous_ecs_service_identity"},
		{name: "computed name", workloads: 1, rows: []querycontract.EntityContent{{RepoID: "repo-api", EntityType: "TerraformResource", EntityName: "aws_ecs_service.api", Metadata: map[string]any{"resource_type": "aws_ecs_service", "unknown_attributes": []string{"name"}, "attributes": map[string]any{"cluster": cluster}}}}, wantReason: "ecs_declaration_unresolved"},
		{name: "counted resource", workloads: 1, rows: []querycontract.EntityContent{{RepoID: "repo-api", EntityType: "TerraformResource", EntityName: "aws_ecs_service.api", Metadata: map[string]any{"resource_type": "aws_ecs_service", "count": "2", "attributes": map[string]any{"name": "api", "cluster": cluster}}}}, wantReason: "ecs_declaration_unresolved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "api"
			if tc.name == "wrong workload" {
				name = "other"
			}
			got, reason := SelectDeclaredECSServiceARN(name, "repo-api", tc.workloads, tc.rows)
			if got != tc.wantARN || reason != tc.wantReason {
				t.Fatalf("got ARN=%q reason=%q, want ARN=%q reason=%q", got, reason, tc.wantARN, tc.wantReason)
			}
		})
	}
}
