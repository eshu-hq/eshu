// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// canonicalEvidenceSource is the Repository evidence_source value the
// projector writes for indexed repositories. The by-state split compares
// it in Go instead of in Cypher: the pinned NornicDB build folds an
// inline `prop = 'literal'` comparison to null for every row (#8689), so
// a Cypher-side boolean would report every repository as stub there.
const canonicalEvidenceSource = "projector/canonical"

// indexStatusRepositoryCountByStateCypher groups Repository nodes by
// evidence source and counts each group. The caller maps the canonical
// group to indexed and every other group to stub. It is the graph half
// of the #7766 repository_count_by_state block; the tombstone half reads
// Postgres. Grouping by the projected property (instead of a Cypher-side
// boolean) keeps the comparison backend-neutral: both backends return
// one row per distinct source with agreeing digests (#8689).
const indexStatusRepositoryCountByStateCypher = `MATCH (r:Repository)
RETURN r.evidence_source AS src, count(*) AS count`

// indexStatusRepositoryCountByState splits the deployment's repository
// count into indexed versus stub from the graph evidence source. A nil
// graph answers zeros without a read, the same historical contract the
// repository_count path keeps for a driverless reader. Absent row keys
// zero-fill: a backend with no repositories returns no rows. Rows
// accumulate, so several groups for one side add up instead of the last
// overwriting the rest.
func indexStatusRepositoryCountByState(ctx context.Context, graph GraphQuery) (map[string]any, error) {
	indexed, stub := 0, 0
	if graph == nil {
		return map[string]any{"indexed": indexed, "stub": stub}, nil
	}
	rows, err := graph.Run(ctx, indexStatusRepositoryCountByStateCypher, nil)
	if err != nil {
		return nil, fmt.Errorf("index status repository count by state: %w", err)
	}
	for _, row := range rows {
		if row == nil {
			return nil, fmt.Errorf("index status repository count by state: count query returned a nil row")
		}
		if StringVal(row, "src") == canonicalEvidenceSource {
			indexed += IntVal(row, "count")
		} else {
			stub += IntVal(row, "count")
		}
	}
	return map[string]any{"indexed": indexed, "stub": stub}, nil
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
