// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// indexStatusRepositoryCountByStateCypher groups Repository nodes by
// canonical evidence: indexed rows carry evidence_source
// 'projector/canonical', every other row reads stub. It is the graph half
// of the #7766 repository_count_by_state block; the tombstone half reads
// Postgres.
const indexStatusRepositoryCountByStateCypher = `MATCH (r:Repository)
WITH r.evidence_source = 'projector/canonical' AS idx
RETURN idx, count(*) AS count`

// indexStatusRepositoryCountByState splits the deployment's repository
// count into indexed versus stub from the graph evidence source. A nil
// graph answers zeros without a read, the same historical contract the
// repository_count path keeps for a driverless reader. Absent row keys
// zero-fill: the backend returns at most one row per side.
func indexStatusRepositoryCountByState(ctx context.Context, graph GraphQuery) (map[string]any, error) {
	counts := map[string]any{"indexed": 0, "stub": 0}
	if graph == nil {
		return counts, nil
	}
	rows, err := graph.Run(ctx, indexStatusRepositoryCountByStateCypher, nil)
	if err != nil {
		return nil, fmt.Errorf("index status repository count by state: %w", err)
	}
	for _, row := range rows {
		if row == nil {
			return nil, fmt.Errorf("index status repository count by state: count query returned a nil row")
		}
		if BoolVal(row, "idx") {
			counts["indexed"] = IntVal(row, "count")
		} else {
			counts["stub"] = IntVal(row, "count")
		}
	}
	return counts, nil
}

// indexRetirementSummaryToMap renders the tombstone aggregate as the
// index-status repository_retirements block (#7766): per-state open-row
// counts, the oldest pending age, and the last completion. Counts only, no
// ids. Absent evidence omits its key rather than fabricating a zero.
func indexRetirementSummaryToMap(summary status.RetirementSummary) map[string]any {
	counts := make(map[string]any, len(summary.CountsByState))
	for state, count := range summary.CountsByState {
		counts[state] = count
	}
	block := map[string]any{"counts_by_state": counts}
	if summary.HasPending {
		block["oldest_pending_age_seconds"] = summary.OldestPendingAgeSeconds
	}
	if summary.HasCompleted {
		block["last_completed_at"] = summary.LastCompletedAt.Format(time.RFC3339)
	}
	return block
}
