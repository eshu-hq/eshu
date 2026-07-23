// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkCloudInventoryRecordFromRowAWSAllowlist measures
// cloudInventoryRecordFromRow (issue #5449) on one representative AWS
// aws_ecs_task row whose attributes carry a mix of allowlisted keys
// (task_definition_arn, containers[]) and dropped raw locators (cluster_arn,
// network_interfaces, desired_status, group, launch_type, started_at). This is
// the readback loader's per-row hot path: cloud inventory admission runs it
// once per source fact row in a scope generation.
func BenchmarkCloudInventoryRecordFromRowAWSAllowlist(b *testing.B) {
	arn := "arn:aws:ecs:us-east-1:000000000000:task/bench-cluster/0000000000000000000000000000000a"
	payload := []byte(`{
		"arn":"` + arn + `",
		"resource_type":"aws_ecs_task",
		"attributes":{
			"cluster_arn":"arn:aws:ecs:us-east-1:000000000000:cluster/bench-cluster",
			"task_definition_arn":"arn:aws:ecs:us-east-1:000000000000:task-definition/bench:1",
			"desired_status":"RUNNING",
			"group":"service:bench",
			"launch_type":"FARGATE",
			"started_at":"2026-01-01T00:00:00Z",
			"network_interfaces":[{"network_interface_id":"eni-000000000000000aa","private_ipv4_address":"10.0.0.5","subnet_id":"subnet-000000000000000aa"}],
			"containers":[
				{"image":"000000000000.dkr.ecr.us-east-1.amazonaws.com/bench:latest","image_digest":"sha256:0000000000000000000000000000000000000000000000000000000000aa","name":"bench","runtime_id":"0000000000000000000000000000000000000000000000000000000000bb"}
			]
		}
	}`)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := cloudInventoryRecordFromRow(facts.AWSResourceFactKind, arn, payload); !ok {
			b.Fatal("aws row unexpectedly dropped")
		}
	}
}

// BenchmarkCloudInventoryRecordFromRowGCPPassthrough measures the same
// per-row loader path for GCP's raw-passthrough attributes (surfacesAttributes
// true, boundedCloudInventoryAttributes) on a comparably-sized payload, as the
// No-Regression Evidence baseline the AWS allowlist path (above) is compared
// against: the allowlist adds a fixed small key-set walk plus one nested-array
// reduction in place of a full-map iteration, so it is not expected to cost
// more than the existing GCP path per row.
func BenchmarkCloudInventoryRecordFromRowGCPPassthrough(b *testing.B) {
	gcpName := "//bigquery.googleapis.com/projects/bench/datasets/d/tables/t"
	payload := []byte(`{
		"full_resource_name":"` + gcpName + `",
		"asset_type":"bigquery.googleapis.com/Table",
		"attributes":{
			"table_type":"TABLE",
			"schema_field_count":12,
			"partitioned":true,
			"clustering_fields":["project_id","date"],
			"stage":"GA"
		}
	}`)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := cloudInventoryRecordFromRow(facts.GCPCloudResourceFactKind, gcpName, payload); !ok {
			b.Fatal("gcp row unexpectedly dropped")
		}
	}
}
