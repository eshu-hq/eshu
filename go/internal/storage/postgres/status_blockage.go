// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

var reducerConflictBlockageQuery = `
WITH ` + activeFactWorkItemsCTE + `,
` + reducerClaimReadinessRequirementsCTE() + `,
eligible AS (
    SELECT work_item_id,
           scope_id,
           generation_id,
           domain,
           conflict_domain,
           COALESCE(conflict_key, scope_id) AS conflict_key,
           COALESCE(visible_at, created_at) AS available_at,
           payload
    FROM active_fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND (visible_at IS NULL OR visible_at <= $1)
      AND (claim_until IS NULL OR claim_until <= $1)
),
blocked AS (
    SELECT eligible.work_item_id,
           eligible.domain,
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
),
readiness_blocked AS (
    -- Surface each missing readiness requirement as its own bounded blockage row.
    -- Multi-key domains such as security-group reachability and EC2 profile
    -- edges appear once per missing keyspace/entity-key requirement.
    SELECT eligible.work_item_id,
           eligible.domain,
           'readiness' AS conflict_domain,
           readiness_req.keyspace || ':' || readiness_req.phase || ':' ||
               ` + reducerClaimReadinessAcceptanceUnitSQL("eligible", "readiness_req") + ` AS conflict_key,
           eligible.available_at
    FROM eligible
    JOIN reducer_claim_readiness_requirements AS readiness_req
      ON readiness_req.domain = eligible.domain
      AND NOT EXISTS (
          SELECT 1
          FROM graph_projection_phase_state AS readiness_phase
          WHERE readiness_phase.scope_id = eligible.scope_id
            AND readiness_phase.acceptance_unit_id = ` + reducerClaimReadinessAcceptanceUnitSQL("eligible", "readiness_req") + `
            AND readiness_phase.source_run_id = eligible.generation_id
            AND readiness_phase.generation_id = eligible.generation_id
            AND readiness_phase.keyspace = readiness_req.keyspace
            AND readiness_phase.phase = readiness_req.phase
      )
),
all_blocked AS (
    SELECT work_item_id, domain, conflict_domain, conflict_key, available_at FROM blocked
    UNION ALL
    SELECT work_item_id, domain, conflict_domain, conflict_key, available_at FROM readiness_blocked
),
blockage_rows AS (
    SELECT domain,
           conflict_domain,
           conflict_key,
           COUNT(DISTINCT work_item_id) AS blocked_count,
           COALESCE(EXTRACT(EPOCH FROM ($1 - MIN(available_at))), 0) AS oldest_blocked_age_seconds
    FROM all_blocked
    GROUP BY domain, conflict_domain, conflict_key
),
domain_blocked AS (
    SELECT domain,
           COUNT(DISTINCT work_item_id) AS domain_blocked_count
    FROM all_blocked
    GROUP BY domain
)
SELECT 'reducer' AS stage,
       domain,
       conflict_domain,
       conflict_key,
       domain_blocked_count AS blocked_count,
       oldest_blocked_age_seconds
FROM blockage_rows
JOIN domain_blocked USING (domain)
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
