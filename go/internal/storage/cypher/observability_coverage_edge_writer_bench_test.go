// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchObservabilityCoverageEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"observability_uid": fmt.Sprintf("obs-%d", i),
			"target_uid":        fmt.Sprintf("tgt-%d", i),
			"coverage_signal":   "alarm",
			"resolution_mode":   "bare_id",
			"scope_id":          "scope-1",
			"generation_id":     "gen-1",
		})
	}
	return rows
}

// BenchmarkObservabilityCoverageEdgeWriter measures the statement-construction
// and batching cost of the COVERS edge writer for a realistic
// per-scope-generation edge count. The backend executor is a no-op so the
// benchmark isolates Eshu-owned write-path work (batched MATCH-MATCH-MERGE row
// shaping) from graph round trips, proving the write side has no N+1.
func BenchmarkObservabilityCoverageEdgeWriter(b *testing.B) {
	rows := benchObservabilityCoverageEdgeRows(5000)
	writer := NewObservabilityCoverageEdgeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteObservabilityCoverageEdges(ctx, rows, "scope-1", "gen-1", "reducer/observability-coverage"); err != nil {
			b.Fatalf("WriteObservabilityCoverageEdges: %v", err)
		}
	}
}
