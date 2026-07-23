// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchCloudResourceContainerImageEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"source_uid":        fmt.Sprintf("src-%d", i),
			"target_uid":        fmt.Sprintf("tgt-%d", i),
			"relationship_type": "lambda_function_uses_image",
			"resolution_mode":   "container_image_digest",
			"scope_id":          "scope-1",
			"generation_id":     "gen-1",
		})
	}
	return rows
}

// BenchmarkCloudResourceContainerImageEdgeWriter measures the
// statement-construction and batching cost of the AWS cloud-image edge writer
// for a realistic per-scope-generation edge count (issue #5450
// prove-theory-first). The backend executor is a no-op so the benchmark
// isolates Eshu-owned write-path work (batched MATCH-MATCH-MERGE row shaping)
// from graph round trips, mirroring BenchmarkCloudResourceEdgeWriter — the
// architecturally identical CloudResource -> CloudResource sibling writer —
// so the two numbers are directly comparable at the same corpus size and
// batch size.
func BenchmarkCloudResourceContainerImageEdgeWriter(b *testing.B) {
	rows := benchCloudResourceContainerImageEdgeRows(5000)
	writer := NewCloudResourceContainerImageEdgeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteCloudResourceContainerImageEdges(ctx, rows, "scope-1", "gen-1", "reducer/aws-cloud-image"); err != nil {
			b.Fatalf("WriteCloudResourceContainerImageEdges: %v", err)
		}
	}
}
