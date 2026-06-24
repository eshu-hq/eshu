// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

// noopGroupExecutor records nothing and returns nil so the benchmark measures
// statement construction and batching cost, not backend round trips.
type noopGroupExecutor struct{}

func (noopGroupExecutor) Execute(context.Context, Statement) error        { return nil }
func (noopGroupExecutor) ExecuteGroup(context.Context, []Statement) error { return nil }

func benchCloudResourceRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":                 fmt.Sprintf("uid-%d", i),
			"arn":                 fmt.Sprintf("arn:aws:ec2:us-east-1:111122223333:vpc/vpc-%d", i),
			"resource_id":         fmt.Sprintf("vpc-%d", i),
			"resource_type":       "aws_ec2_vpc",
			"name":                "main",
			"state":               "available",
			"account_id":          "111122223333",
			"region":              "us-east-1",
			"service_kind":        "vpc",
			"correlation_anchors": []string{fmt.Sprintf("vpc-%d", i)},
			"source_fact_id":      "fact",
			"stable_fact_key":     "key",
			"source_system":       "aws",
			"source_record_id":    "rec",
			"source_confidence":   "reported",
			"collector_kind":      "aws",
		})
	}
	return rows
}

// BenchmarkCloudResourceNodeWriter measures the statement-construction and
// batching cost of the CloudResource node writer for a realistic
// per-scope-generation resource count. The backend executor is a no-op so the
// benchmark isolates Eshu-owned write-path work from graph round trips.
func BenchmarkCloudResourceNodeWriter(b *testing.B) {
	rows := benchCloudResourceRows(5000)
	writer := NewCloudResourceNodeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteCloudResourceNodes(ctx, rows, "reducer/aws-resources"); err != nil {
			b.Fatalf("WriteCloudResourceNodes: %v", err)
		}
	}
}
