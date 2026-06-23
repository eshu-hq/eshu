package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// reducerGraphWriteTimeoutDepthQuery counts reducer work items that are retrying
// specifically because a bounded graph write timed out. It scopes the count to
// failure_class = graph_write_timeout so the producer write-backpressure gate
// (#3560) reacts only to genuine graph-write pressure. Readiness-not-ready
// retrying rows (secrets_iam_endpoint_not_ready and other *_n classes) and
// generic reducer_retryable rows are deliberately excluded so a readiness
// backlog never false-throttles unrelated reducer admission. It reuses the
// active-generation CTE so superseded stale-generation rows do not inflate the
// signal.
const reducerGraphWriteTimeoutDepthQuery = `
WITH ` + activeFactWorkItemsCTE + `
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

const semanticQueueDepthQuery = `
SELECT 'semantic_extraction' AS queue,
       status,
       COUNT(*)::BIGINT AS count
FROM semantic_extraction_jobs
WHERE status IN ('pending', 'claimed', 'retrying')
  AND provider_job = true
GROUP BY status
ORDER BY status
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
`

// QueueObserverStore implements telemetry.QueueObserver by querying the
// fact_work_items table for live queue depth and oldest-item age per stage.
type QueueObserverStore struct {
	queryer Queryer
	Now     func() time.Time
}

// NewQueueObserverStore returns a QueueObserver backed by Postgres.
func NewQueueObserverStore(queryer Queryer) *QueueObserverStore {
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
