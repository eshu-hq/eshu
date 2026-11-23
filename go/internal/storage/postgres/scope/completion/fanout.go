// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package completionstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// manifestMarkerPredicate is true for a supply_chain_impact work item that is a
// manifest marker (payload trigger=manifest_dependency, #9874). A marker
// reads no CI/CD, identity, or finding evidence and writes no finding, so a
// completion event can change nothing it derives: re-pending a succeeded one
// or flagging a claimed one only re-runs a pass that cannot change a finding,
// and flagging one at its ack re-pends it (the migration 093 trigger). Only
// supply_chain_impact items carry the marker, so the domain test keeps the
// JSONB read off every other consumer domain. The spelling comes from the
// supply-chain core constants the projector and the handler share.
const manifestMarkerPredicate = `source.domain = '` + string(reducer.DomainSupplyChainImpact) + `'
      AND COALESCE(source.payload->>'` + supplychaincore.PayloadKeyTrigger + `', '') = '` + supplychaincore.TriggerManifestDependency + `'`

// FanoutCrossScopeCompletionQuery claims one cross-scope completion event
// and fans it out to eligible consumers. Exported for the staying root plan
// tests, which EXPLAIN the shipped query rather than a copy.
const FanoutCrossScopeCompletionQuery = `
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
), dependencies(producer_domain, consumer_domain) AS (
    SELECT producer_domain, consumer_domain
    FROM unnest($7::text[], $8::text[])
         AS edge(producer_domain, consumer_domain)
), ` + activeGenerationsCTE + `, current_consumers AS MATERIALIZED (
    SELECT source.work_item_id,
           source.status,
           source.cross_scope_replay_required
    FROM fact_work_items AS source
    JOIN dependencies AS dependency
      ON dependency.consumer_domain = source.domain
    JOIN lease
      ON lease.producer_domain = dependency.producer_domain
    ` + activeGenerationJoin + `
    WHERE ` + consumerStatePredicate + `
      AND NOT (` + manifestMarkerPredicate + `)
    ORDER BY source.work_item_id COLLATE "C"
    FOR NO KEY UPDATE OF source
), captured AS MATERIALIZED (
    SELECT event.event_id, event.producer_item_count
    FROM cross_scope_completion_events AS event
    CROSS JOIN lease
    WHERE event.producer_domain = lease.producer_domain
      -- Drain the ordered row locks before acquiring queued event locks.
      AND (SELECT count(*) FROM current_consumers) >= 0
      AND (
            event.event_id = lease.event_id
         OR (event.status IN ('pending', 'retrying')
             AND (event.visible_at IS NULL OR event.visible_at <= $1))
      )
    ORDER BY CASE WHEN event.event_id = lease.event_id THEN 0 ELSE 1 END,
             event.event_id
    LIMIT $6
    FOR UPDATE OF event
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
      -- Capture the completion horizon before changing the locked consumers.
      AND (SELECT count(*) FROM captured) > 0
      AND (
            consumer.status = 'succeeded'
         OR (consumer.status IN ('claimed', 'running')
             AND NOT consumer.cross_scope_replay_required)
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
// each current-generation canonical consumer at most once (never a manifest
// marker), and deletes only that captured event set in one statement.
//
// When the producer has a supply_chain_impact consumer edge, Fanout then counts
// the markers it left alone into ManifestMarkersSkipped with a separate
// read-only statement (manifestMarkerCountQuery). That count runs only after
// the fan-out statement returned and committed (autocommit, no lock held), under
// its own bounded context, so it adds nothing to the lock hold of the fan-out
// or to a producer ack waiting on the event row. A count failure is the one
// failure Fanout tolerates: the fan-out already committed and the count only
// feeds eshu_dp_cross_scope_completion_markers_skipped_total, so an error or
// timeout logs a warning, leaves ManifestMarkersSkipped at zero, and returns
// the committed result.
func (s *CrossScopeCompletionStore) Fanout(
	ctx context.Context,
	lease reducer.CrossScopeCompletionLease,
	batchSize int,
) (reducer.CrossScopeCompletionResult, error) {
	if s == nil || s.database == nil {
		return reducer.CrossScopeCompletionResult{}, errors.New("cross-scope completion database is required")
	}
	if batchSize <= 0 {
		return reducer.CrossScopeCompletionResult{}, errors.New("cross-scope completion batch size must be positive")
	}
	edges := crossscope.CompletionEdges()
	producerDomains := make([]string, 0, len(edges))
	consumerDomains := make([]string, 0, len(edges))
	feedsSupplyChainImpact := false
	for _, edge := range edges {
		producerDomains = append(producerDomains, string(edge.Producer))
		consumerDomains = append(consumerDomains, string(edge.Consumer))
		if string(edge.Producer) == string(lease.ProducerDomain) && edge.Consumer == reducer.DomainSupplyChainImpact {
			feedsSupplyChainImpact = true
		}
	}
	result, err := s.runFanoutStatement(ctx, lease, batchSize, producerDomains, consumerDomains)
	if err != nil {
		return reducer.CrossScopeCompletionResult{}, err
	}
	if feedsSupplyChainImpact {
		result.ManifestMarkersSkipped = s.countSkippedManifestMarkers(ctx)
	}
	return result, nil
}

// runFanoutStatement runs the fan-out statement and returns its four counts. It
// closes the cursor before returning so the caller's next statement never
// overlaps an open one on the same connection.
func (s *CrossScopeCompletionStore) runFanoutStatement(
	ctx context.Context,
	lease reducer.CrossScopeCompletionLease,
	batchSize int,
	producerDomains []string,
	consumerDomains []string,
) (reducer.CrossScopeCompletionResult, error) {
	rows, err := s.database.QueryContext(
		ctx,
		FanoutCrossScopeCompletionQuery,
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
