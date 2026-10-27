// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Factless-convergence probe for #9257: counts, per collector kind, the
// completed work items whose generation committed nothing for the reducer
// to project. Split out to keep workflow_run_reconciliation.go under the
// 500-line cap.

// listWorkflowCollectorFactlessCompletedCountsQuery counts, per collector
// kind, the completed work items whose generation committed nothing for the
// reducer to project: no fact_records rows for the item's (scope,
// generation) and no fact_work_items rows for the generation. A completed
// claim whose result carries no facts skips ingestion entirely, so neither
// table ever sees the generation; counting those items as vacuously
// published keeps the run from wedging in reducer_converging (#9257). Both
// NOT EXISTS probes hit (scope_id, generation_id) index prefixes
// (fact_records_scope_generation_idx,
// fact_work_items_scope_generation_idx), so one reconcile pass costs one
// cheap probe per completed item instead of a table scan.
const listWorkflowCollectorFactlessCompletedCountsQuery = `
SELECT
    item.collector_kind,
    COUNT(*) AS factless_completed_items
FROM workflow_work_items AS item
WHERE item.run_id = $1
  AND item.status = 'completed'
  AND NOT EXISTS (
    SELECT 1
    FROM fact_records AS fact
    WHERE fact.scope_id = item.scope_id
      AND fact.generation_id = item.generation_id
  )
  AND NOT EXISTS (
    SELECT 1
    FROM fact_work_items AS work
    WHERE work.scope_id = item.scope_id
      AND work.generation_id = item.generation_id
  )
GROUP BY item.collector_kind
ORDER BY item.collector_kind ASC
`

// listWorkflowCollectorFactlessCompletedCounts loads, per collector kind,
// how many completed work items committed nothing for the reducer to
// project (no fact_records for the item's scope and generation, no
// fact_work_items for the generation). An absent entry means zero, which the
// reconciler treats as "every completed item committed something," never as
// vacuous convergence (#9257).
func (s *WorkflowControlStore) listWorkflowCollectorFactlessCompletedCounts(
	ctx context.Context,
	queryer db.Queryer,
	runID string,
) (map[string]int, error) {
	rows, err := queryer.QueryContext(ctx, listWorkflowCollectorFactlessCompletedCountsQuery, runID)
	if err != nil {
		return nil, fmt.Errorf("list workflow collector factless completed counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	factlessCounts := make(map[string]int)
	for rows.Next() {
		var collectorKind string
		var factlessCount int
		if err := rows.Scan(&collectorKind, &factlessCount); err != nil {
			return nil, fmt.Errorf("list workflow collector factless completed counts: %w", err)
		}
		factlessCounts[strings.TrimSpace(collectorKind)] = factlessCount
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list workflow collector factless completed counts: %w", err)
	}
	return factlessCounts, nil
}
