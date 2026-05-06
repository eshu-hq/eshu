package postgres

import (
	"context"
	"fmt"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

const reducerConflictBlockageQuery = `
WITH eligible AS (
    SELECT work_item_id,
           scope_id,
           domain,
           conflict_domain,
           COALESCE(conflict_key, scope_id) AS conflict_key,
           COALESCE(visible_at, created_at) AS available_at
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND (visible_at IS NULL OR visible_at <= $1)
      AND (claim_until IS NULL OR claim_until <= $1)
),
blocked AS (
    SELECT eligible.domain,
           eligible.conflict_domain,
           eligible.conflict_key,
           eligible.available_at
    FROM eligible
    JOIN fact_work_items AS inflight
      ON inflight.stage = 'reducer'
     AND inflight.conflict_domain = eligible.conflict_domain
     AND COALESCE(inflight.conflict_key, inflight.scope_id) = eligible.conflict_key
     AND inflight.work_item_id <> eligible.work_item_id
     AND inflight.status IN ('claimed', 'running')
     AND inflight.claim_until > $1
)
SELECT 'reducer' AS stage,
       domain,
       conflict_domain,
       conflict_key,
       COUNT(*) AS blocked_count,
       COALESCE(EXTRACT(EPOCH FROM ($1 - MIN(available_at))), 0) AS oldest_blocked_age_seconds
FROM blocked
GROUP BY domain, conflict_domain, conflict_key
ORDER BY blocked_count DESC, oldest_blocked_age_seconds DESC, domain ASC, conflict_key ASC
LIMIT 10
`

// listReducerConflictBlockages reports reducer rows that are otherwise
// claimable but fenced by an active row in the same durable conflict key.
func listReducerConflictBlockages(
	ctx context.Context,
	queryer Queryer,
	asOf time.Time,
) ([]statuspkg.QueueBlockage, error) {
	rows, err := queryer.QueryContext(ctx, reducerConflictBlockageQuery, asOf)
	if err != nil {
		return nil, fmt.Errorf("list reducer conflict blockages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	blockages := []statuspkg.QueueBlockage{}
	for rows.Next() {
		var stage string
		var domain string
		var conflictDomain string
		var conflictKey string
		var blockedCount int64
		var oldestBlockedAgeSeconds float64
		if scanErr := rows.Scan(
			&stage,
			&domain,
			&conflictDomain,
			&conflictKey,
			&blockedCount,
			&oldestBlockedAgeSeconds,
		); scanErr != nil {
			return nil, fmt.Errorf("list reducer conflict blockages: %w", scanErr)
		}
		blockages = append(blockages, statuspkg.QueueBlockage{
			Stage:          stage,
			Domain:         domain,
			ConflictDomain: conflictDomain,
			ConflictKey:    conflictKey,
			Blocked:        int(blockedCount),
			OldestAge:      durationFromSeconds(oldestBlockedAgeSeconds),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list reducer conflict blockages: %w", err)
	}

	return blockages, nil
}
