// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const fanoutCrossScopeCompletionQuery = `
WITH lease AS MATERIALIZED (
    SELECT event_id, producer_domain
    FROM cross_scope_completion_events
    WHERE event_id = $2
      AND producer_domain = $3
      AND lease_owner = $4
      AND claim_epoch = $5
      AND status IN ('claimed', 'running')
      AND claim_until > $1
    FOR UPDATE
), captured AS MATERIALIZED (
    SELECT event.event_id, event.producer_item_count
    FROM cross_scope_completion_events AS event
    CROSS JOIN lease
    WHERE event.producer_domain = lease.producer_domain
      AND (
            event.event_id = lease.event_id
         OR (event.status IN ('pending', 'retrying')
             AND (event.visible_at IS NULL OR event.visible_at <= $1))
      )
    ORDER BY CASE WHEN event.event_id = lease.event_id THEN 0 ELSE 1 END,
             event.event_id
    LIMIT $6
    FOR UPDATE OF event
), dependencies(producer_domain, consumer_domain) AS (
    SELECT producer_domain, consumer_domain
    FROM unnest($7::text[], $8::text[])
         AS edge(producer_domain, consumer_domain)
), current_consumers AS MATERIALIZED (
    SELECT source.work_item_id,
           source.status,
           source.cross_scope_replay_required
    FROM fact_work_items AS source
    JOIN dependencies AS dependency
      ON dependency.consumer_domain = source.domain
    JOIN lease
      ON lease.producer_domain = dependency.producer_domain
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = source.scope_id
     AND scope.active_generation_id = source.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = source.scope_id
     AND generation.generation_id = source.generation_id
     AND generation.status = 'active'
    WHERE source.stage = 'reducer'
      AND source.status IN ('claimed', 'running', 'succeeded')
), scheduled AS (
    UPDATE fact_work_items AS consumer
    SET status = CASE
            WHEN consumer.status = 'succeeded' THEN 'pending'
            ELSE consumer.status
        END,
        attempt_count = CASE
            WHEN consumer.status = 'succeeded' THEN 0
            ELSE consumer.attempt_count
        END,
        lease_owner = CASE
            WHEN consumer.status = 'succeeded' THEN NULL
            ELSE consumer.lease_owner
        END,
        claim_until = CASE
            WHEN consumer.status = 'succeeded' THEN NULL
            ELSE consumer.claim_until
        END,
        visible_at = CASE
            WHEN consumer.status = 'succeeded' THEN $1
            ELSE consumer.visible_at
        END,
        next_attempt_at = CASE
            WHEN consumer.status = 'succeeded' THEN NULL
            ELSE consumer.next_attempt_at
        END,
        updated_at = CASE
            WHEN consumer.status = 'succeeded' THEN $1
            ELSE consumer.updated_at
        END,
        reopened_at = CASE
            WHEN consumer.status = 'succeeded' THEN $1
            ELSE consumer.reopened_at
        END,
        cross_scope_replay_required = CASE
            WHEN consumer.status IN ('claimed', 'running') THEN TRUE
            ELSE FALSE
        END,
        failure_class = CASE
            WHEN consumer.status = 'succeeded' THEN NULL
            ELSE consumer.failure_class
        END,
        failure_message = CASE
            WHEN consumer.status = 'succeeded' THEN NULL
            ELSE consumer.failure_message
        END,
        failure_details = CASE
            WHEN consumer.status = 'succeeded' THEN NULL
            ELSE consumer.failure_details
        END
    FROM current_consumers AS source
    WHERE consumer.work_item_id = source.work_item_id
      AND (
            source.status = 'succeeded'
         OR (source.status IN ('claimed', 'running')
             AND NOT source.cross_scope_replay_required)
      )
    RETURNING 1
), deleted AS (
    DELETE FROM cross_scope_completion_events AS event
    USING captured
    WHERE event.event_id = captured.event_id
    RETURNING 1
)
SELECT (SELECT count(*) FROM captured),
       (SELECT COALESCE(sum(producer_item_count), 0) FROM captured),
       (SELECT count(*) FROM scheduled),
       (SELECT count(*) FROM deleted)
`

// Fanout coalesces an exact pending event set for the leased producer, schedules
// each current-generation canonical consumer at most once, and deletes only
// that captured event set in one statement.
func (s *CrossScopeCompletionStore) Fanout(
	ctx context.Context,
	lease reducer.CrossScopeCompletionLease,
	batchSize int,
) (reducer.CrossScopeCompletionResult, error) {
	if s == nil || s.db == nil {
		return reducer.CrossScopeCompletionResult{}, errors.New("cross-scope completion database is required")
	}
	if batchSize <= 0 {
		return reducer.CrossScopeCompletionResult{}, errors.New("cross-scope completion batch size must be positive")
	}
	edges := reducer.CrossScopeCompletionEdges()
	producerDomains := make([]string, 0, len(edges))
	consumerDomains := make([]string, 0, len(edges))
	for _, edge := range edges {
		producerDomains = append(producerDomains, string(edge.Producer))
		consumerDomains = append(consumerDomains, string(edge.Consumer))
	}
	rows, err := s.db.QueryContext(
		ctx,
		fanoutCrossScopeCompletionQuery,
		s.now(),
		lease.EventID,
		lease.ProducerDomain,
		lease.LeaseOwner,
		lease.ClaimEpoch,
		batchSize,
		producerDomains,
		consumerDomains,
	)
	if err != nil {
		return reducer.CrossScopeCompletionResult{}, fmt.Errorf("fanout cross-scope completion event: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return reducer.CrossScopeCompletionResult{}, fmt.Errorf("fanout cross-scope completion event: %w", err)
		}
		return reducer.CrossScopeCompletionResult{}, ErrCrossScopeCompletionClaimRejected
	}
	var result reducer.CrossScopeCompletionResult
	var deleted int
	if err := rows.Scan(
		&result.EventsProcessed,
		&result.ProducerItemsProcessed,
		&result.IntentsEnqueued,
		&deleted,
	); err != nil {
		return reducer.CrossScopeCompletionResult{}, fmt.Errorf("scan cross-scope completion fanout: %w", err)
	}
	if result.EventsProcessed == 0 || deleted != result.EventsProcessed {
		return reducer.CrossScopeCompletionResult{}, ErrCrossScopeCompletionClaimRejected
	}
	return result, nil
}
