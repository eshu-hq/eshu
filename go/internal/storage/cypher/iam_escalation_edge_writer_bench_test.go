// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchIAMEscalationEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"principal_uid":   fmt.Sprintf("principal-%d", i%256),
			"target_uid":      fmt.Sprintf("target-%d", i),
			"primitives":      []string{"iam_create_policy_version", "iam_attach_role_policy"},
			"primitive_count": 2,
		})
	}
	return rows
}

// BenchmarkIAMEscalationEdgeWriter measures the statement-construction and batching
// cost of the CAN_ESCALATE_TO edge writer for a realistic per-scope-generation edge
// count. The backend executor is a no-op so the benchmark isolates Eshu-owned
// write-path work (batched MERGE row shaping and scope-field stamping) from graph
// round trips, proving the write side has no N+1 and stays in the same shape class
// as the proven RUNS_IMAGE / reachability edge writers: one batched
// MATCH-MATCH-MERGE per chunk over two uid-indexed CloudResource anchors.
func BenchmarkIAMEscalationEdgeWriter(b *testing.B) {
	rows := benchIAMEscalationEdgeRows(5000)
	writer := NewIAMEscalationEdgeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteIAMEscalationEdges(ctx, rows, "scope-1", "gen-1", "reducer/iam-escalation"); err != nil {
			b.Fatalf("WriteIAMEscalationEdges: %v", err)
		}
	}
}
