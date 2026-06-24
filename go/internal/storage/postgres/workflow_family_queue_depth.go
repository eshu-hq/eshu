// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// WorkflowControlStore backs the per-family queue-depth observable gauge.
var _ telemetry.WorkflowFamilyQueueDepthObserver = (*WorkflowControlStore)(nil)

// workflowFamilyQueueDepthQuery counts outstanding workflow work items grouped
// by collector family (collector_kind, source_system) and status. Only
// not-yet-resolved states are counted; completed and terminally-failed rows are
// excluded because they are not live queue depth.
const workflowFamilyQueueDepthQuery = `
SELECT collector_kind,
       source_system,
       status,
       COUNT(*)::BIGINT AS count
FROM workflow_work_items
WHERE status IN ('pending', 'claimed', 'failed_retryable', 'expired')
GROUP BY collector_kind, source_system, status
ORDER BY collector_kind, source_system, status
`

// WorkflowFamilyQueueDepths returns outstanding workflow work-item counts grouped
// as collector_kind -> source_system -> status -> count. It backs the per-family
// queue-depth observable gauge so an operator can see which collector family is
// backing up, using only bounded labels (collector_kind, source_system, status)
// and no high-cardinality identifiers.
func (s *WorkflowControlStore) WorkflowFamilyQueueDepths(ctx context.Context) (map[string]map[string]map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, workflowFamilyQueueDepthQuery)
	if err != nil {
		return nil, fmt.Errorf("workflow family queue depths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]map[string]map[string]int64)
	for rows.Next() {
		var collectorKind, sourceSystem, status string
		var count int64
		if err := rows.Scan(&collectorKind, &sourceSystem, &status, &count); err != nil {
			return nil, fmt.Errorf("workflow family queue depths scan: %w", err)
		}
		if result[collectorKind] == nil {
			result[collectorKind] = make(map[string]map[string]int64)
		}
		if result[collectorKind][sourceSystem] == nil {
			result[collectorKind][sourceSystem] = make(map[string]int64)
		}
		result[collectorKind][sourceSystem][status] += count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("workflow family queue depths: %w", err)
	}
	return result, nil
}
