// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/affected"
)

// isValueFlowRefreshProducer reports the four producer domains whose ACKs
// feed the value-flow refresh emit gate (issue #6785): workload, USES,
// CAN_PERFORM, and aws_resource materialization.
func isValueFlowRefreshProducer(domain reducer.Domain) bool {
	switch domain {
	case reducer.DomainWorkloadMaterialization,
		reducer.DomainWorkloadCloudRelationshipMaterialization,
		reducer.DomainIAMCanPerformMaterialization,
		reducer.DomainAWSResourceMaterialization:
		return true
	default:
		return false
	}
}

// shouldEmitValueFlowRefresh applies the affected-repo emit gate to one
// producer run: positive canonical writes plus an absent (fail-open) or
// positive affected-repo signal.
func shouldEmitValueFlowRefresh(result reducer.Result) bool {
	return affected.ShouldEmitRefresh(result.CanonicalWrites, result.SubSignals)
}

// ackValueFlowRefreshProducerReducerWorkQuery ACKs one refresh-producer work
// item and, when $6 is true, emits its domain's completion event in the same
// statement. Beside the domain predicate ($5) and the emit CTE it bumps
// cross_scope_completion_ack_epoch like the other emitting ACKs, so the
// rolling-upgrade fallback trigger (which fires only on a preserved epoch)
// never double-emits a new-path ACK — including a gate-suppressed one.
const ackValueFlowRefreshProducerReducerWorkQuery = `
WITH acknowledged AS MATERIALIZED (
    UPDATE fact_work_items AS work
    SET status = 'succeeded', lease_owner = NULL, claim_until = NULL,
        provenance_edge_identity_upgrade_required = FALSE,
        cross_scope_completion_ack_epoch = cross_scope_completion_ack_epoch + 1,
        visible_at = NULL, updated_at = $1,
        failure_class = NULL, failure_message = NULL, failure_details = NULL
    WHERE work.work_item_id = $2
      AND stage = 'reducer'
      AND domain = $5
      AND lease_owner = $3
      AND status IN ('claimed', 'running')
      AND claim_until > clock_timestamp()
      AND last_attempt_at = $4
    RETURNING work.work_item_id, work.status
), emission_clock AS MATERIALIZED (
    SELECT clock_timestamp() AS emitted_at
), emitted AS (
    INSERT INTO cross_scope_completion_events (
        producer_domain, producer_item_count, status,
        visible_at, created_at, updated_at
    )
    SELECT $5, count(*)::BIGINT, 'pending',
           emission_clock.emitted_at + INTERVAL '250 milliseconds',
           emission_clock.emitted_at, emission_clock.emitted_at
    FROM acknowledged CROSS JOIN emission_clock
    WHERE acknowledged.status = 'succeeded'
      AND $6
    GROUP BY emission_clock.emitted_at
    ON CONFLICT (producer_domain) WHERE status IN ('pending', 'retrying') DO UPDATE SET
        producer_item_count = cross_scope_completion_events.producer_item_count + EXCLUDED.producer_item_count,
        visible_at = CASE
            WHEN cross_scope_completion_events.status = 'retrying'
                THEN cross_scope_completion_events.visible_at
            ELSE LEAST(cross_scope_completion_events.created_at + INTERVAL '2 seconds', EXCLUDED.visible_at)
        END,
        updated_at = EXCLUDED.updated_at
    RETURNING 1
)
SELECT 1 FROM acknowledged
`

// ackValueFlowRefreshProducerReducerWorkBatchQuery ACKs one domain's
// refresh-producer batch and emits a completion event covering only the
// emit-approved ids ($6). Items outside $6 still transition to succeeded;
// with an empty $6 the INSERT selects no rows, so no event row appears.
func ackValueFlowRefreshProducerReducerWorkBatchQuery(
	now time.Time,
	leaseOwner string,
	domain string,
	ids []string,
	claimedAts []time.Time,
	emitIDs []string,
) (string, []any) {
	return `
WITH requested AS MATERIALIZED (
    SELECT id, claimed_at, 'reducer'::text AS expected_stage,
           $5::text AS expected_domain
	FROM unnest($3::text[], $4::timestamptz[]) AS input(id, claimed_at)
	GROUP BY id, claimed_at
    ORDER BY id COLLATE "C"
), locked_work AS MATERIALIZED (
    SELECT lookup.work_item_id
    FROM requested
    CROSS JOIN LATERAL (
        SELECT work_item_id
        FROM fact_work_items
        WHERE work_item_id = requested.id
          AND stage = requested.expected_stage
          AND domain = requested.expected_domain
          AND lease_owner = $2
          AND status IN ('claimed', 'running')
		  AND claim_until > clock_timestamp()
		  AND last_attempt_at = requested.claimed_at
        FOR NO KEY UPDATE
    ) AS lookup
), acknowledged AS MATERIALIZED (
UPDATE fact_work_items AS work
SET status = 'succeeded',
    cross_scope_completion_ack_epoch = cross_scope_completion_ack_epoch + 1,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE work.work_item_id IN (SELECT work_item_id FROM locked_work)
  AND (SELECT count(*) FROM locked_work) > 0
RETURNING work.work_item_id, work.status
), emission_clock AS MATERIALIZED (
    SELECT clock_timestamp() AS emitted_at
), emitted AS (
INSERT INTO cross_scope_completion_events (
    producer_domain, producer_item_count, status,
    visible_at, created_at, updated_at
)
SELECT $5, count(*)::BIGINT, 'pending',
       emission_clock.emitted_at + INTERVAL '250 milliseconds',
       emission_clock.emitted_at, emission_clock.emitted_at
FROM acknowledged
CROSS JOIN emission_clock
WHERE acknowledged.status = 'succeeded'
  AND acknowledged.work_item_id = ANY($6::text[])
GROUP BY emission_clock.emitted_at
ON CONFLICT (producer_domain) WHERE status IN ('pending', 'retrying') DO UPDATE SET
    producer_item_count = cross_scope_completion_events.producer_item_count + EXCLUDED.producer_item_count,
    visible_at = CASE
        WHEN cross_scope_completion_events.status = 'retrying'
            THEN cross_scope_completion_events.visible_at
        ELSE LEAST(cross_scope_completion_events.created_at + INTERVAL '2 seconds', EXCLUDED.visible_at)
    END,
    updated_at = EXCLUDED.updated_at
RETURNING 1
)
SELECT 1 FROM acknowledged
`, []any{now, leaseOwner, ids, claimedAts, domain, emitIDs}
}

// valueFlowRefreshAckGroup is one domain's batch slice: every id is ACKed,
// only emitIDs contribute to the completion event.
type valueFlowRefreshAckGroup struct {
	domain     reducer.Domain
	ids        []string
	claimedAts []time.Time
	emitIDs    []string
}

// splitValueFlowRefreshAckIntents pulls refresh-producer items out of the
// unrelated batch tail and groups them by domain in sorted order (stable
// statement sequence). Emit membership comes from each item's paired result.
func splitValueFlowRefreshAckIntents(
	unrelated []reducer.Intent,
	resultByID map[string]reducer.Result,
) ([]reducer.Intent, []valueFlowRefreshAckGroup) {
	kept := make([]reducer.Intent, 0, len(unrelated))
	byDomain := make(map[reducer.Domain]*valueFlowRefreshAckGroup)
	for _, intent := range unrelated {
		if !isValueFlowRefreshProducer(intent.Domain) {
			kept = append(kept, intent)
			continue
		}
		group, ok := byDomain[intent.Domain]
		if !ok {
			group = &valueFlowRefreshAckGroup{domain: intent.Domain}
			byDomain[intent.Domain] = group
		}
		group.ids = append(group.ids, intent.IntentID)
		group.claimedAts = append(group.claimedAts, claimedAtValue(intent))
		if shouldEmitValueFlowRefresh(resultByID[intent.IntentID]) {
			group.emitIDs = append(group.emitIDs, intent.IntentID)
		}
	}
	domains := make([]reducer.Domain, 0, len(byDomain))
	for domain := range byDomain {
		domains = append(domains, domain)
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i] < domains[j] })
	groups := make([]valueFlowRefreshAckGroup, 0, len(domains))
	for _, domain := range domains {
		groups = append(groups, *byDomain[domain])
	}
	return kept, groups
}
