// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchS3LogsToEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"source_uid":        fmt.Sprintf("src-%d", i),
			"target_uid":        fmt.Sprintf("tgt-%d", i),
			"relationship_type": "LOGS_TO",
			"resolution_mode":   "name",
		})
	}
	return rows
}

// BenchmarkS3LogsToEdgeWriter measures the statement-construction and batching
// cost of the S3 LOGS_TO edge writer for a realistic per-scope-generation edge
// count. The backend executor is a no-op so the benchmark isolates Eshu-owned
// write-path work (batched MATCH-MATCH-MERGE row shaping) from graph round trips,
// proving the write side has no N+1. The write shape is the same closed-vocab
// static-token MATCH-MATCH-MERGE as the shipped CAN_ASSUME writer (#1134 PR2),
// so this is the no-regression baseline for the LOGS_TO slice.
func BenchmarkS3LogsToEdgeWriter(b *testing.B) {
	rows := benchS3LogsToEdgeRows(5000)
	writer := NewS3LogsToEdgeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteS3LogsToEdges(ctx, rows, "scope-1", "gen-1", "reducer/s3-logs-to"); err != nil {
			b.Fatalf("WriteS3LogsToEdges: %v", err)
		}
	}
}
