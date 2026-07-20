// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// repoDependencyBenchmarkRowsPerType is the row count per canonical edge type
// in BenchmarkEdgeWriterRepoDependencyWrite, chosen to match the existing
// BenchmarkEdgeWriterCodeCallRetractAndWrite scale (#5441 evidence: same
// order of magnitude as a full-repository CALLS refresh).
const repoDependencyBenchmarkRowsPerType = 1000

// BenchmarkEdgeWriterRepoDependencyWrite isolates Eshu-owned row shaping and
// Cypher batching for the five canonical repository relationship edges
// (DEPLOYS_FROM, DISCOVERS_CONFIG_IN, PROVISIONS_DEPENDENCY_FOR, USES_MODULE,
// READS_CONFIG_FROM) behind a no-op executor, so results isolate
// buildRowMap/copyRepoRelationshipMetadata and UNWIND batching from backend
// round trips. This is the #5441 before/after proof point: the change only
// adds three SET-clause row keys to copyRepoRelationshipMetadata and the
// batch Cypher templates, so the batch/MERGE shape is unchanged.
func BenchmarkEdgeWriterRepoDependencyWrite(b *testing.B) {
	rows := benchmarkRepoDependencyRows(repoDependencyBenchmarkRowsPerType)
	writer := NewEdgeWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteEdges(ctx, reducer.DomainRepoDependency, rows, "resolver/cross-repo"); err != nil {
			b.Fatalf("WriteEdges: %v", err)
		}
	}
	b.ReportMetric(float64(len(rows)), "input_rows/op")
}

func benchmarkRepoDependencyRows(rowsPerType int) []reducer.SharedProjectionIntentRow {
	types := []string{
		"DEPLOYS_FROM",
		"DISCOVERS_CONFIG_IN",
		"PROVISIONS_DEPENDENCY_FOR",
		"USES_MODULE",
		"READS_CONFIG_FROM",
	}
	rows := make([]reducer.SharedProjectionIntentRow, 0, rowsPerType*len(types))
	for _, relType := range types {
		for i := 0; i < rowsPerType; i++ {
			rows = append(rows, reducer.SharedProjectionIntentRow{
				IntentID:     fmt.Sprintf("%s-%d", relType, i),
				RepositoryID: "repo-a",
				GenerationID: "gen-1",
				Payload: map[string]any{
					"repo_id":                 "repo-a",
					"target_repo_id":          fmt.Sprintf("repo-target-%d", i),
					"relationship_type":       relType,
					"evidence_type":           "argocd_application_source",
					"resolved_id":             fmt.Sprintf("resolved-%d", i),
					"generation_id":           "gen-1",
					"evidence_count":          3,
					"evidence_kinds":          []string{"ARGOCD_APPLICATION_SOURCE"},
					"resolution_source":       "inferred",
					"confidence":              0.9,
					"rationale":               "benchmark fixture",
					"source_revision":         "main",
					"destination_namespace":   "prod",
					"first_party_ref_version": "v1.2.3",
				},
			})
		}
	}
	return rows
}
