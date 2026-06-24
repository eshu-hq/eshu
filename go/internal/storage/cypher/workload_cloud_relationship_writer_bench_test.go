// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchWorkloadCloudRelationshipRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"workload_id":           fmt.Sprintf("workload:svc-%d", i),
			"cloud_resource_uid":    fmt.Sprintf("aws-resource-%d", i),
			"relationship_type":     "USES",
			"resolution_mode":       "explicit_workload_anchor",
			"environment":           "prod",
			"relationship_basis":    "aws_resource_service_anchor",
			"service_anchor_source": "payload.workload_id",
			"service_anchor_reason": "explicit_workload_anchor",
			"source_fact_id":        fmt.Sprintf("fact-%d", i),
			"stable_fact_key":       fmt.Sprintf("stable-fact-%d", i),
			"source_system":         "aws",
			"source_record_id":      fmt.Sprintf("source-%d", i),
			"collector_kind":        "aws_cloud",
		})
	}
	return rows
}

// BenchmarkWorkloadCloudRelationshipWriter measures statement construction and
// batching for a same-shape 5000-row, batch-500 workload-cloud edge write. The
// backend executor is a no-op group executor so the benchmark isolates
// Eshu-owned row annotation and batched MATCH-MATCH-MERGE dispatch cost from
// graph round trips.
func BenchmarkWorkloadCloudRelationshipWriter(b *testing.B) {
	rows := benchWorkloadCloudRelationshipRows(5000)
	writer := NewWorkloadCloudRelationshipWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteWorkloadCloudRelationshipEdges(ctx, rows, "scope-1", "gen-1", "reducer/workload-cloud-relationship"); err != nil {
			b.Fatalf("WriteWorkloadCloudRelationshipEdges: %v", err)
		}
	}
}
