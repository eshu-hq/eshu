// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchIAMCanPerformEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"principal_uid":    fmt.Sprintf("principal-%d", i%256),
			"resource_uid":     fmt.Sprintf("resource-%d", i),
			"actions":          []string{"s3:getobject", "s3:putobject"},
			"action_count":     2,
			"evaluation_scope": "identity_policy_only",
		})
	}
	return rows
}

// BenchmarkIAMCanPerformEdgeWriter measures the statement-construction and batching
// cost of the CAN_PERFORM edge writer for a realistic per-scope-generation edge
// count. The backend executor is a no-op so the benchmark isolates Eshu-owned
// write-path work (batched MERGE row shaping and scope-field stamping) from graph
// round trips, proving the write side has no N+1 and stays in the same shape class
// as the shipped CAN_ESCALATE_TO writer (BenchmarkIAMEscalationEdgeWriter), the
// no-regression baseline on the identical 5000-row shape: one batched
// MATCH-MATCH-MERGE per chunk over two uid-indexed CloudResource anchors.
func BenchmarkIAMCanPerformEdgeWriter(b *testing.B) {
	rows := benchIAMCanPerformEdgeRows(5000)
	writer := NewIAMCanPerformEdgeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteIAMCanPerformEdges(ctx, rows, "scope-1", "gen-1", "reducer/iam-can-perform"); err != nil {
			b.Fatalf("WriteIAMCanPerformEdges: %v", err)
		}
	}
}
