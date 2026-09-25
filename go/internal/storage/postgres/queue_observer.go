// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres //nolint:dirgate // #6693 checklist step 38 moves this file to queue/reducer/observer.go; not yet executed

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// reducerGraphWriteTimeoutDepthQuery counts reducer work items that are retrying
// specifically because a bounded graph write timed out. It scopes the count to
// failure_class = graph_write_timeout so the producer write-backpressure gate
// (#3560) reacts only to genuine graph-write pressure. Readiness-not-ready
// retrying rows (secrets_iam_endpoint_not_ready and other *_n classes) and
// generic reducer_retryable rows are deliberately excluded so a readiness
// backlog never false-throttles unrelated reducer admission. The per-row
// active-generation filter (selective, so no per-scope build per poll; #6794)
// keeps superseded stale-generation rows from inflating the signal.
const reducerGraphWriteTimeoutDepthQuery = `
WITH ` + activeFactWorkItemsPerRowCTE + `
SELECT COUNT(*) AS count
FROM active_fact_work_items
WHERE stage = 'reducer'
  AND status = 'retrying'
  AND failure_class = '` + cypher.GraphWriteTimeoutFailureClass + `'
`

const queueDepthQuery = `
WITH ` + activeFactWorkItemsCTE + `
SELECT stage,
       status,
       COUNT(*) AS count
FROM active_fact_work_items
WHERE status IN ('pending', 'claimed', 'running', 'retrying')
GROUP BY stage, status
ORDER BY stage, status
`

const queueOldestAgeQuery = `
WITH ` + activeFactWorkItemsCTE + `
SELECT stage,
       COALESCE(
         EXTRACT(
           EPOCH FROM (
             $1 - MIN(created_at)
               FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying'))
           )
         ),
         0
       ) AS oldest_age_seconds
FROM active_fact_work_items
WHERE status IN ('pending', 'claimed', 'running', 'retrying')
GROUP BY stage
`

const sourceQueueDepthQuery = `
WITH ` + activeFactWorkItemsCTE + `
SELECT work.stage,
       CASE
         WHEN work.stage = 'projector' THEN COALESCE(NULLIF(BTRIM(scope.source_system), ''), 'unknown')
         ELSE COALESCE(NULLIF(BTRIM(work.payload->>'source_system'), ''), NULLIF(BTRIM(scope.source_system), ''), 'unknown')
       END AS source_system,
       work.status,
       COUNT(*) AS count
FROM active_fact_work_items AS work
JOIN ingestion_scopes AS scope
  ON scope.scope_id = work.scope_id
WHERE work.status IN ('pending', 'claimed', 'running', 'retrying')
GROUP BY 1, 2, work.status
ORDER BY work.stage, source_system, work.status
`

const sourceQueueOldestAgeQuery = `
WITH ` + activeFactWorkItemsCTE + `
SELECT work.stage,
       CASE
         WHEN work.stage = 'projector' THEN COALESCE(NULLIF(BTRIM(scope.source_system), ''), 'unknown')
         ELSE COALESCE(NULLIF(BTRIM(work.payload->>'source_system'), ''), NULLIF(BTRIM(scope.source_system), ''), 'unknown')
       END AS source_system,
       COALESCE(
         EXTRACT(
           EPOCH FROM (
             $1 - MIN(work.created_at)
               FILTER (WHERE work.status IN ('pending', 'claimed', 'running', 'retrying'))
           )
         ),
         0
       ) AS oldest_age_seconds
FROM active_fact_work_items AS work
JOIN ingestion_scopes AS scope
  ON scope.scope_id = work.scope_id
WHERE work.status IN ('pending', 'claimed', 'running', 'retrying')
GROUP BY 1, 2
ORDER BY work.stage, source_system
`

const semanticQueueDepthQuery = `
SELECT 'semantic_extraction' AS queue,
       status,
       COUNT(*)::BIGINT AS count
FROM semantic_extraction_jobs
WHERE status IN ('pending', 'claimed', 'retrying')
  AND provider_job = true
GROUP BY status
UNION ALL
SELECT 'cross_scope_completion.' || producer_domain AS queue,
       status,
       COUNT(*)::BIGINT AS count
FROM cross_scope_completion_events
WHERE status IN ('pending', 'claimed', 'running', 'retrying')
GROUP BY producer_domain, status
ORDER BY queue, status
`

const semanticQueueOldestAgeQuery = `
SELECT 'semantic_extraction' AS queue,
       GREATEST(
         COALESCE(
           EXTRACT(
             EPOCH FROM (
               $1 - MIN(created_at)
                 FILTER (WHERE status IN ('pending', 'claimed', 'retrying')
                         AND provider_job = true)
             )
           ),
           0
         ),
         0
       ) AS oldest_age_seconds
FROM semantic_extraction_jobs
WHERE status IN ('pending', 'claimed', 'retrying')
  AND provider_job = true
HAVING COUNT(*) > 0
UNION ALL
SELECT 'cross_scope_completion.' || producer_domain AS queue,
       GREATEST(
         COALESCE(EXTRACT(EPOCH FROM ($1 - MIN(created_at))), 0),
         0
       ) AS oldest_age_seconds
FROM cross_scope_completion_events
WHERE status IN ('pending', 'claimed', 'running', 'retrying')
GROUP BY producer_domain
HAVING COUNT(*) > 0
`

// projectorScopesWithMultipleLiveLeasesQuery counts projector scopes that hold
// more than one unexpired claimed or running lease at $1 (#7115). The claim's
// scope fence keeps this at zero. It reads live leases only, so an expired
// lease awaiting reclaim beside a live one does not count.
const projectorScopesWithMultipleLiveLeasesQuery = `
SELECT COUNT(*) AS count
FROM (
    SELECT scope_id
    FROM fact_work_items
    WHERE stage = 'projector'
      AND status IN ('claimed', 'running')
      AND claim_until > $1
    GROUP BY scope_id
    HAVING COUNT(*) > 1
) AS overlapping
`

// QueueObserverStore implements telemetry.QueueObserver by querying the
// fact_work_items table for live queue depth and oldest-item age per stage.
type QueueObserverStore struct {
	queryer db.Queryer
	Now     func() time.Time
}

// NewQueueObserverStore returns a QueueObserver backed by Postgres.
func NewQueueObserverStore(queryer db.Queryer) *QueueObserverStore {
	return &QueueObserverStore{queryer: queryer}
}

func (s *QueueObserverStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// QueueDepths returns queue depth per stage and status. The returned map
// uses stage name as the outer key and status as the inner key. Status
// values "claimed" and "running" are merged into "in_flight" to match
// the operator mental model.
func (s *QueueObserverStore) QueueDepths(ctx context.Context) (map[string]map[string]int64, error) {
	if s.queryer == nil {
		return nil, fmt.Errorf("queue observer queryer is required")
	}

	result := make(map[string]map[string]int64)
	if err := s.addQueueDepthRows(ctx, result, queueDepthQuery); err != nil {
		return nil, err
	}
	if err := s.addQueueDepthRows(ctx, result, semanticQueueDepthQuery); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *QueueObserverStore) addQueueDepthRows(
	ctx context.Context,
	result map[string]map[string]int64,
	query string,
) error {
	rows, err := s.queryer.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("queue depths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var stage, status string
		var count int64
		if err := rows.Scan(&stage, &status, &count); err != nil {
			return fmt.Errorf("queue depths scan: %w", err)
		}
		if result[stage] == nil {
			result[stage] = make(map[string]int64)
		}
		// Merge claimed+running into in_flight for the operator gauge.
		switch status {
		case "claimed", "running":
			result[stage]["in_flight"] += count
		default:
			result[stage][status] += count
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("queue depths: %w", err)
	}

	return nil
}

// SourceQueueDepths returns queue depth per stage, source system, and status.
// Status values "claimed" and "running" are merged into "in_flight" to match
// QueueDepths while keeping source_system bounded to collector/source families.
func (s *QueueObserverStore) SourceQueueDepths(ctx context.Context) (map[string]map[string]map[string]int64, error) {
	if s.queryer == nil {
		return nil, fmt.Errorf("queue observer queryer is required")
	}

	rows, err := s.queryer.QueryContext(ctx, sourceQueueDepthQuery)
	if err != nil {
		return nil, fmt.Errorf("source queue depths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]map[string]map[string]int64)
	for rows.Next() {
		var queue, sourceSystem, status string
		var count int64
		if err := rows.Scan(&queue, &sourceSystem, &status, &count); err != nil {
			return nil, fmt.Errorf("source queue depths scan: %w", err)
		}
		if result[queue] == nil {
			result[queue] = make(map[string]map[string]int64)
		}
		if result[queue][sourceSystem] == nil {
			result[queue][sourceSystem] = make(map[string]int64)
		}
		switch status {
		case "claimed", "running":
			result[queue][sourceSystem]["in_flight"] += count
		default:
			result[queue][sourceSystem][status] += count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("source queue depths: %w", err)
	}

	return result, nil
}

// ProjectorScopesWithMultipleLiveLeases returns how many scopes hold more than
// one unexpired claimed or running projector lease. It implements
// telemetry.ProjectorLeaseInvariantObserver; any nonzero value means two
// workers are projecting one scope at once (#7115).
func (s *QueueObserverStore) ProjectorScopesWithMultipleLiveLeases(ctx context.Context) (int64, error) {
	if s.queryer == nil {
		return 0, fmt.Errorf("queue observer queryer is required")
	}
	rows, err := s.queryer.QueryContext(ctx, projectorScopesWithMultipleLiveLeasesQuery, s.now())
	if err != nil {
		return 0, fmt.Errorf("projector scopes with multiple live leases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var count int64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("projector scopes with multiple live leases scan: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("projector scopes with multiple live leases: %w", err)
	}
	return count, nil
}

// ReducerGraphWriteTimeoutDepth returns the number of reducer work items that
// are retrying because a bounded graph write timed out (failure_class =
// graph_write_timeout). The producer write-backpressure gate consumes this
// count so it defers admission only under genuine graph-write pressure, never on
// readiness-not-ready backlogs that also persist as retrying rows (#3560).
func (s *QueueObserverStore) ReducerGraphWriteTimeoutDepth(ctx context.Context) (int64, error) {
	if s.queryer == nil {
		return 0, fmt.Errorf("queue observer queryer is required")
	}

	rows, err := s.queryer.QueryContext(ctx, reducerGraphWriteTimeoutDepthQuery)
	if err != nil {
		return 0, fmt.Errorf("reducer graph-write-timeout depth: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var depth int64
	if rows.Next() {
		if err := rows.Scan(&depth); err != nil {
			return 0, fmt.Errorf("reducer graph-write-timeout depth scan: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("reducer graph-write-timeout depth: %w", err)
	}

	return depth, nil
}

// SourceQueueOldestAge returns oldest outstanding item age by stage and source.
func (s *QueueObserverStore) SourceQueueOldestAge(ctx context.Context) (map[string]map[string]float64, error) {
	if s.queryer == nil {
		return nil, fmt.Errorf("queue observer queryer is required")
	}

	rows, err := s.queryer.QueryContext(ctx, sourceQueueOldestAgeQuery, s.now())
	if err != nil {
		return nil, fmt.Errorf("source queue oldest age: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]map[string]float64)
	for rows.Next() {
		var queue, sourceSystem string
		var ageSeconds float64
		if err := rows.Scan(&queue, &sourceSystem, &ageSeconds); err != nil {
			return nil, fmt.Errorf("source queue oldest age scan: %w", err)
		}
		if result[queue] == nil {
			result[queue] = make(map[string]float64)
		}
		result[queue][sourceSystem] = ageSeconds
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("source queue oldest age: %w", err)
	}

	return result, nil
}

// QueueOldestAge returns the age in seconds of the oldest outstanding item
// per stage (queue).
func (s *QueueObserverStore) QueueOldestAge(ctx context.Context) (map[string]float64, error) {
	if s.queryer == nil {
		return nil, fmt.Errorf("queue observer queryer is required")
	}

	now := s.now()
	result := make(map[string]float64)
	if err := s.addQueueOldestAgeRows(ctx, result, queueOldestAgeQuery, now); err != nil {
		return nil, err
	}
	if err := s.addQueueOldestAgeRows(ctx, result, semanticQueueOldestAgeQuery, now); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *QueueObserverStore) addQueueOldestAgeRows(
	ctx context.Context,
	result map[string]float64,
	query string,
	now time.Time,
) error {
	rows, err := s.queryer.QueryContext(ctx, query, now)
	if err != nil {
		return fmt.Errorf("queue oldest age: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var stage string
		var ageSeconds float64
		if err := rows.Scan(&stage, &ageSeconds); err != nil {
			return fmt.Errorf("queue oldest age scan: %w", err)
		}
		result[stage] = ageSeconds
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("queue oldest age: %w", err)
	}

	return nil
}
