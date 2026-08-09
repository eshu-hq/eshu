// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BenchmarkEdgeWriterUnroutableRowLoop isolates the row loop's not-routed
// branch, which the repo-scale benchmarks cannot: in
// BenchmarkEdgeWriterRepoDependencyWrite every row routes, so the added
// reducer.CarriesNoEdge call and the drop counters are never reached, and any
// regression in them would hide behind the Cypher-building and batching cost of
// 5,000 routable rows.
//
// Review finding on PR #6008. The repo-scale suite showed no significant change
// (geomean +0.14%, every case ~ at n=8), but "no significant change on a
// benchmark that does not execute the branch" is not evidence about the branch.
//
// The three cases separate the costs that actually differ per row:
//
//   - all_routable: the baseline path, where the new code adds nothing but a
//     not-taken branch.
//   - all_unroutable_edge_rows: every row pays CarriesNoEdge plus the counter
//     bookkeeping, then the batch returns the error without building Cypher.
//   - all_control_rows: every row pays CarriesNoEdge and is correctly NOT
//     counted, the case that must stay cheap because a deleted-files-only
//     delta hits it on every poll.
func BenchmarkEdgeWriterUnroutableRowLoop(b *testing.B) {
	const rowCount = 5000

	routable := make([]reducer.SharedProjectionIntentRow, 0, rowCount)
	unroutable := make([]reducer.SharedProjectionIntentRow, 0, rowCount)
	control := make([]reducer.SharedProjectionIntentRow, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		id := fmt.Sprintf("i%d", i)
		routable = append(routable, reducer.SharedProjectionIntentRow{
			IntentID:     id,
			RepositoryID: "repo-a",
			Payload:      map[string]any{"repo_id": "repo-a", "target_repo_id": "repo-b"},
		})
		unroutable = append(unroutable, reducer.SharedProjectionIntentRow{
			IntentID:     id,
			RepositoryID: "repo-a",
			Payload:      map[string]any{"repo_id": "", "target_repo_id": "repo-b"},
		})
		control = append(control, reducer.SharedProjectionIntentRow{
			IntentID:     id,
			RepositoryID: "repo-a",
			Payload:      map[string]any{"repo_id": "repo-a", "intent_type": "repo_refresh"},
		})
	}

	cases := []struct {
		name string
		rows []reducer.SharedProjectionIntentRow
	}{
		{"all_routable", routable},
		{"all_unroutable_edge_rows", unroutable},
		{"all_control_rows", control},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			writer := NewEdgeWriter(noopGroupExecutor{}, 500)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// The unroutable case returns an error by design; the loop cost
				// is what is being measured, so the result is discarded rather
				// than asserted here (the behaviour has its own tests).
				_, _ = writer.WriteEdges(ctx, reducer.DomainRepoDependency, tc.rows, "finalization/workloads")
			}
			b.ReportMetric(float64(len(tc.rows)), "input_rows/op")
		})
	}
}
