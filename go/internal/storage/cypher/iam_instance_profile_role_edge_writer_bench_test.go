// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchIAMInstanceProfileRoleEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"profile_uid":       fmt.Sprintf("profile-%d", i),
			"role_uid":          fmt.Sprintf("role-%d", i),
			"relationship_type": "HAS_ROLE",
			"resolution_mode":   "arn",
		})
	}
	return rows
}

// BenchmarkIAMInstanceProfileRoleEdgeWriter measures the statement-construction
// and batching cost of the IAM instance-profile HAS_ROLE edge writer for a
// realistic per-scope-generation edge count. The backend executor is a no-op so
// the benchmark isolates Eshu-owned write-path work from graph round trips.
func BenchmarkIAMInstanceProfileRoleEdgeWriter(b *testing.B) {
	rows := benchIAMInstanceProfileRoleEdgeRows(5000)
	writer := NewIAMInstanceProfileRoleEdgeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteIAMInstanceProfileRoleEdges(ctx, rows, "scope-1", "gen-1", "reducer/iam-instance-profile-role"); err != nil {
			b.Fatalf("WriteIAMInstanceProfileRoleEdges: %v", err)
		}
	}
}
