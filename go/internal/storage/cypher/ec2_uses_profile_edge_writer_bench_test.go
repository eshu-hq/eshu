// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchEC2UsesProfileEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"source_uid":        fmt.Sprintf("ec2-%d", i),
			"target_uid":        fmt.Sprintf("profile-%d", i),
			"relationship_type": "USES_PROFILE",
			"resolution_mode":   "arn",
		})
	}
	return rows
}

// BenchmarkEC2UsesProfileEdgeWriter measures the statement-construction and
// batching cost of the EC2 USES_PROFILE edge writer for a realistic
// per-scope-generation edge count. The backend executor is a no-op so the
// benchmark isolates Eshu-owned write-path work (batched MATCH-MATCH-MERGE row
// shaping) from graph round trips, proving the write side has no N+1. The write
// shape is the same closed-vocab static-token MATCH-MATCH-MERGE as the shipped S3
// LOGS_TO writer (#1144 PR2) and CAN_ASSUME writer (#1134 PR2), so
// BenchmarkS3LogsToEdgeWriter on the identical 5000-row / batch-500 / no-op group
// executor shape is the no-regression baseline for this slice.
func BenchmarkEC2UsesProfileEdgeWriter(b *testing.B) {
	rows := benchEC2UsesProfileEdgeRows(5000)
	writer := NewEC2UsesProfileEdgeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteEC2UsesProfileEdges(ctx, rows, "scope-1", "gen-1", "reducer/ec2-uses-profile"); err != nil {
			b.Fatalf("WriteEC2UsesProfileEdges: %v", err)
		}
	}
}
