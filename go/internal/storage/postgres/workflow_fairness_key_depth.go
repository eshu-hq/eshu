// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// WorkflowControlStore backs the fair-claim fairness-key depth observer.
var _ telemetry.WorkflowFairnessKeyDepthObserver = (*WorkflowControlStore)(nil)

// workflowFairnessKeyDepthQueryFormat counts pending workflow work items
// grouped by collector family and fairness key, keeping the top N keys per
// family by pending depth and reporting each family's distinct pending key
// count alongside. The top-N bound is enforced in SQL (ROW_NUMBER), so the
// depth gauge never sees an unbounded key set; N is the telemetry contract's
// WorkflowFairnessTopKeys, substituted at call time rather than frozen here.
const workflowFairnessKeyDepthQueryFormat = `
WITH per_key AS (
    SELECT collector_kind,
           collector_instance_id,
           COALESCE(fairness_key, '') AS fairness_key,
           COUNT(*) AS pending
    FROM workflow_work_items
    WHERE status = 'pending'
    GROUP BY collector_kind, collector_instance_id, COALESCE(fairness_key, '')
),
ranked AS (
    SELECT collector_kind,
           collector_instance_id,
           fairness_key,
           pending,
           COUNT(*) OVER (
               PARTITION BY collector_kind, collector_instance_id
           ) AS distinct_keys,
           ROW_NUMBER() OVER (
               PARTITION BY collector_kind, collector_instance_id
               ORDER BY pending DESC, fairness_key
           ) AS rn
    FROM per_key
)
SELECT collector_kind, collector_instance_id, fairness_key, pending, distinct_keys
FROM ranked
WHERE rn <= %d
ORDER BY collector_kind, collector_instance_id, pending DESC, fairness_key
`

// WorkflowFairnessKeyDepths returns pending fairness-key depth grouped as
// collector_kind -> collector_instance_id -> family depth, with at most
// telemetry.WorkflowFairnessTopKeys keys per family. It backs the fairness
// depth and key-count gauges so an operator can see which key of a family
// is backing up and how many keys the round-robin bound currently spans.
func (s *WorkflowControlStore) WorkflowFairnessKeyDepths(ctx context.Context) (map[string]map[string]telemetry.WorkflowFairnessFamilyDepth, error) {
	rows, err := s.database.QueryContext(ctx, fmt.Sprintf(workflowFairnessKeyDepthQueryFormat, telemetry.WorkflowFairnessTopKeys))
	if err != nil {
		return nil, fmt.Errorf("workflow fairness key depths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]map[string]telemetry.WorkflowFairnessFamilyDepth)
	for rows.Next() {
		var collectorKind, instanceID, key string
		var pending, distinctKeys int64
		if err := rows.Scan(&collectorKind, &instanceID, &key, &pending, &distinctKeys); err != nil {
			return nil, fmt.Errorf("workflow fairness key depths scan: %w", err)
		}
		if result[collectorKind] == nil {
			result[collectorKind] = make(map[string]telemetry.WorkflowFairnessFamilyDepth)
		}
		family := result[collectorKind][instanceID]
		family.DistinctKeys = distinctKeys
		family.Top = append(family.Top, telemetry.WorkflowFairnessKeyPending{Key: key, Pending: pending})
		result[collectorKind][instanceID] = family
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("workflow fairness key depths: %w", err)
	}
	return result, nil
}
