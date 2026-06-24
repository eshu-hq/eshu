// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchEC2InstanceNodeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":                         fmt.Sprintf("ec2-uid-%d", i),
			"arn":                         fmt.Sprintf("arn:aws:ec2:us-east-1:111122223333:instance/i-%012d", i),
			"resource_id":                 fmt.Sprintf("i-%012d", i),
			"resource_type":               "aws_ec2_instance",
			"name":                        fmt.Sprintf("i-%012d", i),
			"state":                       "running",
			"account_id":                  "111122223333",
			"region":                      "us-east-1",
			"service_kind":                "ec2",
			"correlation_anchors":         []string{fmt.Sprintf("i-%012d", i)},
			"imds_v2_required":            true,
			"imds_http_endpoint":          "enabled",
			"imds_http_put_hop_limit":     int32(1),
			"user_data_present":           false,
			"detailed_monitoring_enabled": false,
			"ebs_optimized":               true,
			"public_ip_associated":        true,
			"instance_profile_arn":        "arn:aws:iam::111122223333:instance-profile/app",
			"tenancy":                     "default",
			"nitro_enclave_enabled":       false,
			"source_fact_id":              "fact",
			"stable_fact_key":             "key",
			"source_system":               "aws",
			"source_record_id":            "rec",
			"source_confidence":           "reported",
			"collector_kind":              "awscloud",
		})
	}
	return rows
}

// BenchmarkEC2InstanceNodeWriter measures the statement-construction and batching
// cost of the EC2 instance node writer for a realistic per-account/region
// instance count. The backend executor is a no-op so the benchmark isolates
// Eshu-owned write-path work from graph round trips. The shape mirrors
// BenchmarkCloudResourceNodeWriter so the two canonical CloudResource node
// writers stay on the same measured baseline: the EC2 writer adds only the ten
// derived posture SET properties to the identical batched MERGE-on-uid shape.
func BenchmarkEC2InstanceNodeWriter(b *testing.B) {
	rows := benchEC2InstanceNodeRows(5000)
	writer := NewEC2InstanceNodeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteEC2InstanceNodes(ctx, rows, "reducer/ec2-instances"); err != nil {
			b.Fatalf("WriteEC2InstanceNodes: %v", err)
		}
	}
}
