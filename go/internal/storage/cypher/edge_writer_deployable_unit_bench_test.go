// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func benchDeployableUnitEdgeRows(n int) []reducer.SharedProjectionIntentRow {
	rows := make([]reducer.SharedProjectionIntentRow, 0, n)
	for i := 0; i < n; i++ {
		repoID := fmt.Sprintf("repo-service-%d", i)
		unitKey := fmt.Sprintf("service-%d", i)
		rows = append(rows, reducer.SharedProjectionIntentRow{
			IntentID:     fmt.Sprintf("intent-%d", i),
			RepositoryID: repoID,
			GenerationID: "generation-1",
			Payload: map[string]any{
				"repo_id":             repoID,
				"deployment_repo_id":  fmt.Sprintf("repo-deployments-%d", i%100),
				"deployable_unit_key": unitKey,
				"correlation_key":     repoID + ":" + unitKey,
				"confidence":          0.94,
				"evidence_count":      9,
				"evidence_kinds":      []string{"argocd", "deployment_repo", "deployable_unit_key", "repository_identity"},
				"generation_id":       "generation-1",
				"rule_pack":           "argocd",
				"admission_state":     "admitted",
				"reason":              "admitted deployable unit correlation",
			},
		})
	}
	return rows
}

// BenchmarkDeployableUnitCorrelationEdgeWriter measures the statement
// construction and batching cost for deployable-unit graph truth. The executor
// is a no-op, so the benchmark isolates Eshu-owned row shaping from graph round
// trips and compares directly with the existing shared EdgeWriter baselines.
func BenchmarkDeployableUnitCorrelationEdgeWriter(b *testing.B) {
	rows := benchDeployableUnitEdgeRows(5000)
	writer := NewEdgeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteEdges(ctx, reducer.DomainDeployableUnitEdges, rows, "reducer/deployable-unit-correlation"); err != nil {
			b.Fatalf("WriteEdges: %v", err)
		}
	}
}
