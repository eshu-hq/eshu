// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchCloudResourceEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"source_uid":        fmt.Sprintf("src-%d", i),
			"target_uid":        fmt.Sprintf("tgt-%d", i),
			"relationship_type": "USES_KMS_KEY",
			"target_type":       "aws_kms_key",
			"resolution_mode":   "arn",
			"scope_id":          "scope-1",
			"generation_id":     "gen-1",
		})
	}
	return rows
}

// BenchmarkCloudResourceEdgeWriter measures the statement-construction and
// batching cost of the AWS relationship edge writer for a realistic
// per-scope-generation edge count. The backend executor is a no-op so the
// benchmark isolates Eshu-owned write-path work (batched MATCH-MATCH-MERGE row
// shaping) from graph round trips, proving the write side has no N+1.
func BenchmarkCloudResourceEdgeWriter(b *testing.B) {
	rows := benchCloudResourceEdgeRows(5000)
	writer := NewCloudResourceEdgeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteCloudResourceEdges(ctx, rows, "scope-1", "gen-1", "reducer/aws-relationships"); err != nil {
			b.Fatalf("WriteCloudResourceEdges: %v", err)
		}
	}
}
