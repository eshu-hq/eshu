// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchKubernetesCorrelationEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"workload_uid":    fmt.Sprintf("k8s://prod/apps/v1/deployments/ns/w-%d", i),
			"source_uid":      fmt.Sprintf("oci-descriptor://reg/repo@sha256:%064d", i),
			"source_label":    "OciImageManifest",
			"rel_type":        "RUNS_IMAGE",
			"resolution_mode": "digest",
		})
	}
	return rows
}

// BenchmarkKubernetesCorrelationEdgeWriter measures the statement-construction and
// batching cost of the RUNS_IMAGE edge writer for a realistic
// per-scope-generation edge count. The backend executor is a no-op so the
// benchmark isolates Eshu-owned write-path work (batched MATCH-MATCH-MERGE row
// shaping grouped by source label) from graph round trips, proving the write side
// has no N+1. It mirrors BenchmarkObservabilityCoverageEdgeWriter so the
// no-regression comparison is against an established edge-writer baseline on the
// same input shape.
func BenchmarkKubernetesCorrelationEdgeWriter(b *testing.B) {
	rows := benchKubernetesCorrelationEdgeRows(5000)
	writer := NewKubernetesCorrelationEdgeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteKubernetesCorrelationEdges(ctx, rows, "scope-1", "gen-1", "reducer/kubernetes-correlation"); err != nil {
			b.Fatalf("WriteKubernetesCorrelationEdges: %v", err)
		}
	}
}
