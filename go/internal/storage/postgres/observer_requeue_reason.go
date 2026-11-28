// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
)

// requeueReasonDepthQuery breaks the live queue down by stage, status, and
// requeue reason over the same active-rows CTE that powers queue depth
// (#11615). NULL reasons (rows never requeued, or predating the column)
// read as legacy_unclassified: that series is the rollout-ramp proof and
// must trend to ~0 for reducer rows as the fleet rolls.
const requeueReasonDepthQuery = `
WITH ` + activeFactWorkItemsCTE + `
SELECT stage,
       status,
       COALESCE(last_requeue_reason, 'legacy_unclassified') AS reason,
       COUNT(*) AS count
FROM active_fact_work_items
WHERE status IN ('pending', 'claimed', 'running', 'retrying')
GROUP BY stage, status, COALESCE(last_requeue_reason, 'legacy_unclassified')
ORDER BY stage, status, reason
`

// requeueReasonOldestAgeQuery reports the oldest outstanding-item age in
// seconds by requeue reason, over the same active-rows CTE (#11615). NULL
// reasons read as legacy_unclassified.
const requeueReasonOldestAgeQuery = `
WITH ` + activeFactWorkItemsCTE + `
SELECT COALESCE(last_requeue_reason, 'legacy_unclassified') AS reason,
       COALESCE(
         EXTRACT(
           EPOCH FROM (
             $1 - MIN(COALESCE(reopened_at, created_at))
               FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying'))
           )
         ),
         0
       ) AS oldest_age_seconds
FROM active_fact_work_items
WHERE status IN ('pending', 'claimed', 'running', 'retrying')
GROUP BY COALESCE(last_requeue_reason, 'legacy_unclassified')
`

// RequeueReasonDepths returns live queue depth by stage, status, and
// requeue reason. It implements telemetry.RequeueReasonObserver: rows whose
// reason is NULL (never requeued, or predating the #11615 column) read as
// legacy_unclassified. Status values "claimed" and "running" are merged
// into "in_flight" to match QueueDepths while keeping the reason breakdown.
func (s *QueueObserverStore) RequeueReasonDepths(ctx context.Context) (map[string]map[string]map[string]int64, error) {
	if s.queryer == nil {
		return nil, fmt.Errorf("queue observer queryer is required")
	}

	rows, err := s.queryer.QueryContext(ctx, requeueReasonDepthQuery)
	if err != nil {
		return nil, fmt.Errorf("requeue reason depths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]map[string]map[string]int64)
	for rows.Next() {
		var stage, status, reason string
		var count int64
		if err := rows.Scan(&stage, &status, &reason, &count); err != nil {
			return nil, fmt.Errorf("requeue reason depths scan: %w", err)
		}
		if result[stage] == nil {
			result[stage] = make(map[string]map[string]int64)
		}
		switch status {
		case "claimed", "running":
			status = "in_flight"
		}
		if result[stage][status] == nil {
			result[stage][status] = make(map[string]int64)
		}
		result[stage][status][reason] += count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("requeue reason depths: %w", err)
	}

	return result, nil
}

// RequeueReasonOldestAge returns the oldest outstanding-item age in seconds
// by requeue reason. It implements telemetry.RequeueReasonObserver.
func (s *QueueObserverStore) RequeueReasonOldestAge(ctx context.Context) (map[string]float64, error) {
	if s.queryer == nil {
		return nil, fmt.Errorf("queue observer queryer is required")
	}

	rows, err := s.queryer.QueryContext(ctx, requeueReasonOldestAgeQuery, s.now())
	if err != nil {
		return nil, fmt.Errorf("requeue reason oldest age: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]float64)
	for rows.Next() {
		var reason string
		var ageSeconds float64
		if err := rows.Scan(&reason, &ageSeconds); err != nil {
			return nil, fmt.Errorf("requeue reason oldest age scan: %w", err)
		}
		result[reason] = ageSeconds
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("requeue reason oldest age: %w", err)
	}

	return result, nil
}
