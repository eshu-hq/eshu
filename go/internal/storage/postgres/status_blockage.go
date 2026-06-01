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
           generation_id,
           domain,
           conflict_domain,
           COALESCE(conflict_key, scope_id) AS conflict_key,
           COALESCE(visible_at, created_at) AS available_at,
           payload
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
),
readiness_blocked AS (
    SELECT eligible.domain,
           'readiness' AS conflict_domain,
           'cloud_resource_uid:canonical_nodes_committed:' ||
               COALESCE(NULLIF(eligible.payload->>'entity_key', ''), eligible.scope_id) AS conflict_key,
           eligible.available_at
    FROM eligible
    WHERE eligible.domain IN ('aws_relationship_materialization', 'observability_coverage_materialization', 'iam_can_assume_materialization')
      AND NOT EXISTS (
          SELECT 1
          FROM graph_projection_phase_state AS aws_nodes
          WHERE aws_nodes.scope_id = eligible.scope_id
            AND aws_nodes.acceptance_unit_id = COALESCE(NULLIF(eligible.payload->>'entity_key', ''), eligible.scope_id)
            AND aws_nodes.source_run_id = eligible.generation_id
            AND aws_nodes.generation_id = eligible.generation_id
            AND aws_nodes.keyspace = 'cloud_resource_uid'
            AND aws_nodes.phase = 'canonical_nodes_committed'
      )
),
kubernetes_readiness_blocked AS (
    SELECT eligible.domain,
           'readiness' AS conflict_domain,
           'kubernetes_workload_uid:canonical_nodes_committed:' ||
               COALESCE(NULLIF(eligible.payload->>'entity_key', ''), eligible.scope_id) AS conflict_key,
           eligible.available_at
    FROM eligible
    WHERE eligible.domain = 'kubernetes_correlation_materialization'
      AND NOT EXISTS (
          SELECT 1
          FROM graph_projection_phase_state AS kube_nodes
          WHERE kube_nodes.scope_id = eligible.scope_id
            AND kube_nodes.acceptance_unit_id = COALESCE(NULLIF(eligible.payload->>'entity_key', ''), eligible.scope_id)
            AND kube_nodes.source_run_id = eligible.generation_id
            AND kube_nodes.generation_id = eligible.generation_id
            AND kube_nodes.keyspace = 'kubernetes_workload_uid'
            AND kube_nodes.phase = 'canonical_nodes_committed'
      )
),
security_group_reachability_readiness_blocked AS (
    -- The reachability edge (#1135 PR2b) gates on three keyspaces; surface each
    -- missing phase as its own blockage row so an operator can see which node
    -- family (rule / endpoint / cloud_resource) is the one that has not committed.
    SELECT eligible.domain,
           'readiness' AS conflict_domain,
           missing.keyspace || ':canonical_nodes_committed:' ||
               COALESCE(NULLIF(eligible.payload->>'entity_key', ''), eligible.scope_id) AS conflict_key,
           eligible.available_at
    FROM eligible
    CROSS JOIN (VALUES
        ('security_group_rule_uid'),
        ('security_group_endpoint_uid'),
        ('cloud_resource_uid')
    ) AS missing(keyspace)
    WHERE eligible.domain = 'security_group_reachability_materialization'
      AND NOT EXISTS (
          SELECT 1
          FROM graph_projection_phase_state AS sg_nodes
          WHERE sg_nodes.scope_id = eligible.scope_id
            AND sg_nodes.acceptance_unit_id = COALESCE(NULLIF(eligible.payload->>'entity_key', ''), eligible.scope_id)
            AND sg_nodes.source_run_id = eligible.generation_id
            AND sg_nodes.generation_id = eligible.generation_id
            AND sg_nodes.keyspace = missing.keyspace
            AND sg_nodes.phase = 'canonical_nodes_committed'
      )
),
all_blocked AS (
    SELECT domain, conflict_domain, conflict_key, available_at FROM blocked
    UNION ALL
    SELECT domain, conflict_domain, conflict_key, available_at FROM readiness_blocked
    UNION ALL
    SELECT domain, conflict_domain, conflict_key, available_at FROM kubernetes_readiness_blocked
    UNION ALL
    SELECT domain, conflict_domain, conflict_key, available_at FROM security_group_reachability_readiness_blocked
)
SELECT 'reducer' AS stage,
       domain,
       conflict_domain,
       conflict_key,
       COUNT(*) AS blocked_count,
       COALESCE(EXTRACT(EPOCH FROM ($1 - MIN(available_at))), 0) AS oldest_blocked_age_seconds
FROM all_blocked
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
