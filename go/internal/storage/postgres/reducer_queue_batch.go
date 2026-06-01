package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const claimReducerWorkBatchQuery = `
WITH candidate AS (
    SELECT work_item_id
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND (visible_at IS NULL OR visible_at <= $1)
      AND (claim_until IS NULL OR claim_until <= $1)
      AND ($2::text[] IS NULL OR domain = ANY($2::text[]))
      -- NornicDB local_authoritative first-generation runs must not let
      -- reducer graph writes contend with source-local canonical projection
      -- for the same scope. Unrelated scopes can continue draining.
      AND ($5 = false OR NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS projector_work
          WHERE projector_work.stage = 'projector'
            AND projector_work.scope_id = fact_work_items.scope_id
            AND projector_work.status IN ('pending', 'retrying', 'claimed', 'running')
      ))
      -- Semantic entity materialization writes labels onto source-local
      -- content-entity nodes. On NornicDB, running those writes while any
      -- source-local projection is still active causes cross-scope graph
      -- backend contention and retry storms; non-semantic reducer domains can
      -- still drain once their own scope has passed the gate above.
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS projector_any
          WHERE projector_any.stage = 'projector'
            AND projector_any.domain = 'source_local'
            AND projector_any.status IN ('pending', 'retrying', 'claimed', 'running')
      ))
      -- In local-host watch mode the ingester discovers and enqueues source
      -- projector work incrementally. A temporary enqueue gap is not proof
      -- that the whole corpus has drained, so semantic reducers can also wait
      -- for the owner-discovered source-local success count.
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR $6 <= 0 OR (
          SELECT count(*)
          FROM fact_work_items AS projector_done
          WHERE projector_done.stage = 'projector'
            AND projector_done.domain = 'source_local'
            AND projector_done.status = 'succeeded'
      ) >= $6)
      -- NornicDB's semantic label update path is backed by the same label/uid
      -- indexes touched by source-local canonical entities. Eight concurrent
      -- semantic writers have repeatedly timed out on tiny row sets, so cap
      -- only this reducer domain while preserving concurrency for unrelated
      -- reducer domains.
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR (
          SELECT count(*)
          FROM fact_work_items AS semantic_inflight
          WHERE semantic_inflight.stage = 'reducer'
            AND semantic_inflight.domain = 'semantic_entity_materialization'
            AND semantic_inflight.work_item_id <> fact_work_items.work_item_id
            AND semantic_inflight.status IN ('claimed', 'running')
            AND semantic_inflight.claim_until > $1
      ) < $7)
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR (
          SELECT count(*)
          FROM fact_work_items AS semantic_next
          WHERE semantic_next.stage = 'reducer'
            AND semantic_next.domain = 'semantic_entity_materialization'
            AND semantic_next.status IN ('pending', 'retrying', 'claimed', 'running')
            AND (semantic_next.visible_at IS NULL OR semantic_next.visible_at <= $1)
            AND (semantic_next.claim_until IS NULL OR semantic_next.claim_until <= $1)
            AND (
                semantic_next.updated_at < fact_work_items.updated_at
                OR (
                    semantic_next.updated_at = fact_work_items.updated_at
                    AND semantic_next.work_item_id <= fact_work_items.work_item_id
                )
            )
      ) <= $7 - (
          SELECT count(*)
          FROM fact_work_items AS semantic_inflight
          WHERE semantic_inflight.stage = 'reducer'
            AND semantic_inflight.domain = 'semantic_entity_materialization'
            AND semantic_inflight.status IN ('claimed', 'running')
            AND semantic_inflight.claim_until > $1
      ))
      -- AWS relationship edges, observability COVERS edges, and IAM CAN_ASSUME
      -- trust edges all consume CloudResource nodes produced by the
      -- aws_resource_materialization domain for the exact same
      -- scope/generation/entity-key readiness slice. Keep those graph-write
      -- domains pending or retrying until canonical nodes are visibly committed
      -- instead of claiming them and recording retryable reducer failures.
      AND (domain NOT IN ('aws_relationship_materialization', 'observability_coverage_materialization', 'iam_can_assume_materialization') OR EXISTS (
          SELECT 1
          FROM graph_projection_phase_state AS aws_nodes
          WHERE aws_nodes.scope_id = fact_work_items.scope_id
            AND aws_nodes.acceptance_unit_id = COALESCE(NULLIF(fact_work_items.payload->>'entity_key', ''), fact_work_items.scope_id)
            AND aws_nodes.source_run_id = fact_work_items.generation_id
            AND aws_nodes.generation_id = fact_work_items.generation_id
            AND aws_nodes.keyspace = 'cloud_resource_uid'
            AND aws_nodes.phase = 'canonical_nodes_committed'
      ))
      -- The live-workload RUNS_IMAGE edge consumes KubernetesWorkload nodes
      -- produced by the kubernetes_workload_materialization domain for the exact
      -- same scope/generation/entity-key readiness slice, but on the
      -- kubernetes_workload_uid keyspace (a different node family than the AWS and
      -- observability edges above). Keep the edge domain pending or retrying until
      -- those workload nodes are visibly committed instead of claiming it and
      -- recording a retryable reducer failure.
      AND (domain <> 'kubernetes_correlation_materialization' OR EXISTS (
          SELECT 1
          FROM graph_projection_phase_state AS kube_nodes
          WHERE kube_nodes.scope_id = fact_work_items.scope_id
            AND kube_nodes.acceptance_unit_id = COALESCE(NULLIF(fact_work_items.payload->>'entity_key', ''), fact_work_items.scope_id)
            AND kube_nodes.source_run_id = fact_work_items.generation_id
            AND kube_nodes.generation_id = fact_work_items.generation_id
            AND kube_nodes.keyspace = 'kubernetes_workload_uid'
            AND kube_nodes.phase = 'canonical_nodes_committed'
      ))
      -- The security-group reachability edge (#1135 PR2b Option D) gates on three
      -- node families for the same readiness slice: the :SecurityGroupRule nodes,
      -- the CidrBlock/PrefixList endpoint nodes, and the SecurityGroup
      -- CloudResource nodes. Keep the edge domain pending until all three commit.
      AND (domain <> 'security_group_reachability_materialization' OR (
          EXISTS (
              SELECT 1 FROM graph_projection_phase_state AS sg_rule_nodes
              WHERE sg_rule_nodes.scope_id = fact_work_items.scope_id
                AND sg_rule_nodes.acceptance_unit_id = COALESCE(NULLIF(fact_work_items.payload->>'entity_key', ''), fact_work_items.scope_id)
                AND sg_rule_nodes.source_run_id = fact_work_items.generation_id
                AND sg_rule_nodes.generation_id = fact_work_items.generation_id
                AND sg_rule_nodes.keyspace = 'security_group_rule_uid'
                AND sg_rule_nodes.phase = 'canonical_nodes_committed'
          )
          AND EXISTS (
              SELECT 1 FROM graph_projection_phase_state AS sg_endpoint_nodes
              WHERE sg_endpoint_nodes.scope_id = fact_work_items.scope_id
                AND sg_endpoint_nodes.acceptance_unit_id = COALESCE(NULLIF(fact_work_items.payload->>'entity_key', ''), fact_work_items.scope_id)
                AND sg_endpoint_nodes.source_run_id = fact_work_items.generation_id
                AND sg_endpoint_nodes.generation_id = fact_work_items.generation_id
                AND sg_endpoint_nodes.keyspace = 'security_group_endpoint_uid'
                AND sg_endpoint_nodes.phase = 'canonical_nodes_committed'
          )
          AND EXISTS (
              SELECT 1 FROM graph_projection_phase_state AS sg_cloud_nodes
              WHERE sg_cloud_nodes.scope_id = fact_work_items.scope_id
                AND sg_cloud_nodes.acceptance_unit_id = COALESCE(NULLIF(fact_work_items.payload->>'entity_key', ''), fact_work_items.scope_id)
                AND sg_cloud_nodes.source_run_id = fact_work_items.generation_id
                AND sg_cloud_nodes.generation_id = fact_work_items.generation_id
                AND sg_cloud_nodes.keyspace = 'cloud_resource_uid'
                AND sg_cloud_nodes.phase = 'canonical_nodes_committed'
          )
      ))
      -- Reducer domains can touch the same graph nodes for a scope. Fence by
      -- explicit conflict key so unrelated graph families can still overlap.
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS inflight
          WHERE inflight.stage = 'reducer'
            AND inflight.conflict_domain = fact_work_items.conflict_domain
            AND COALESCE(inflight.conflict_key, inflight.scope_id) = COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)
            AND inflight.work_item_id <> fact_work_items.work_item_id
            AND inflight.status IN ('claimed', 'running')
            AND inflight.claim_until > $1
      )
      -- A batch claim must not claim two ready rows for the same conflict key
      -- in one transaction, or the worker pool can race them immediately.
      AND work_item_id = (
          SELECT same.work_item_id
          FROM fact_work_items AS same
          WHERE same.stage = 'reducer'
            AND same.conflict_domain = fact_work_items.conflict_domain
            AND COALESCE(same.conflict_key, same.scope_id) = COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)
            AND same.status IN ('pending', 'retrying', 'claimed', 'running')
            AND (same.visible_at IS NULL OR same.visible_at <= $1)
            AND (same.claim_until IS NULL OR same.claim_until <= $1)
            AND ($2::text[] IS NULL OR same.domain = ANY($2::text[]))
            AND (same.domain NOT IN ('aws_relationship_materialization', 'observability_coverage_materialization', 'iam_can_assume_materialization') OR EXISTS (
                SELECT 1
                FROM graph_projection_phase_state AS same_nodes
                WHERE same_nodes.scope_id = same.scope_id
                  AND same_nodes.acceptance_unit_id = COALESCE(NULLIF(same.payload->>'entity_key', ''), same.scope_id)
                  AND same_nodes.source_run_id = same.generation_id
                  AND same_nodes.generation_id = same.generation_id
                  AND same_nodes.keyspace = 'cloud_resource_uid'
                  AND same_nodes.phase = 'canonical_nodes_committed'
            ))
            AND (same.domain <> 'kubernetes_correlation_materialization' OR EXISTS (
                SELECT 1
                FROM graph_projection_phase_state AS same_kube_nodes
                WHERE same_kube_nodes.scope_id = same.scope_id
                  AND same_kube_nodes.acceptance_unit_id = COALESCE(NULLIF(same.payload->>'entity_key', ''), same.scope_id)
                  AND same_kube_nodes.source_run_id = same.generation_id
                  AND same_kube_nodes.generation_id = same.generation_id
                  AND same_kube_nodes.keyspace = 'kubernetes_workload_uid'
                  AND same_kube_nodes.phase = 'canonical_nodes_committed'
            ))
            AND (same.domain <> 'security_group_reachability_materialization' OR (
                EXISTS (
                    SELECT 1 FROM graph_projection_phase_state AS same_sg_rule_nodes
                    WHERE same_sg_rule_nodes.scope_id = same.scope_id
                      AND same_sg_rule_nodes.acceptance_unit_id = COALESCE(NULLIF(same.payload->>'entity_key', ''), same.scope_id)
                      AND same_sg_rule_nodes.source_run_id = same.generation_id
                      AND same_sg_rule_nodes.generation_id = same.generation_id
                      AND same_sg_rule_nodes.keyspace = 'security_group_rule_uid'
                      AND same_sg_rule_nodes.phase = 'canonical_nodes_committed'
                )
                AND EXISTS (
                    SELECT 1 FROM graph_projection_phase_state AS same_sg_endpoint_nodes
                    WHERE same_sg_endpoint_nodes.scope_id = same.scope_id
                      AND same_sg_endpoint_nodes.acceptance_unit_id = COALESCE(NULLIF(same.payload->>'entity_key', ''), same.scope_id)
                      AND same_sg_endpoint_nodes.source_run_id = same.generation_id
                      AND same_sg_endpoint_nodes.generation_id = same.generation_id
                      AND same_sg_endpoint_nodes.keyspace = 'security_group_endpoint_uid'
                      AND same_sg_endpoint_nodes.phase = 'canonical_nodes_committed'
                )
                AND EXISTS (
                    SELECT 1 FROM graph_projection_phase_state AS same_sg_cloud_nodes
                    WHERE same_sg_cloud_nodes.scope_id = same.scope_id
                      AND same_sg_cloud_nodes.acceptance_unit_id = COALESCE(NULLIF(same.payload->>'entity_key', ''), same.scope_id)
                      AND same_sg_cloud_nodes.source_run_id = same.generation_id
                      AND same_sg_cloud_nodes.generation_id = same.generation_id
                      AND same_sg_cloud_nodes.keyspace = 'cloud_resource_uid'
                      AND same_sg_cloud_nodes.phase = 'canonical_nodes_committed'
                )
            ))
          ORDER BY same.updated_at ASC, same.work_item_id ASC
          LIMIT 1
      )
    ORDER BY updated_at ASC, work_item_id ASC
    LIMIT $8
    FOR UPDATE SKIP LOCKED
),
claimed AS (
    UPDATE fact_work_items AS work
    SET status = 'claimed',
        attempt_count = work.attempt_count + 1,
        lease_owner = $3,
        claim_until = $4,
        last_attempt_at = $1,
        updated_at = $1
    FROM candidate
    WHERE work.work_item_id = candidate.work_item_id
    RETURNING
        work.work_item_id,
        work.scope_id,
        work.generation_id,
        work.domain,
        work.attempt_count,
        work.created_at,
        COALESCE(work.visible_at, work.created_at) AS available_at,
        work.payload
)
SELECT
    work_item_id,
    scope_id,
    generation_id,
    domain,
    attempt_count,
    created_at,
    available_at,
    payload
FROM claimed
`

// ClaimBatch claims up to limit reducer work items in a single Postgres
// round-trip using FOR UPDATE SKIP LOCKED. Implements reducer.BatchWorkSource.
func (q ReducerQueue) ClaimBatch(ctx context.Context, limit int) ([]reducer.Intent, error) {
	if err := q.validateClaim(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 16
	}

	now := q.now()
	rows, err := q.db.QueryContext(
		ctx,
		claimReducerWorkBatchQuery,
		now,
		q.claimDomainFilters(),
		q.LeaseOwner,
		now.Add(q.LeaseDuration),
		q.RequireProjectorDrainBeforeClaim,
		q.ExpectedSourceLocalProjectors,
		q.semanticEntityClaimLimit(),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("batch claim reducer work: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var intents []reducer.Intent
	for rows.Next() {
		intent, err := scanReducerIntent(rows)
		if err != nil {
			return nil, fmt.Errorf("batch claim scan: %w", err)
		}
		intents = append(intents, intent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("batch claim reducer work: %w", err)
	}

	return intents, nil
}

// AckBatch acknowledges multiple claimed reducer work items in a single
// round-trip. Implements reducer.BatchWorkSink.
func (q ReducerQueue) AckBatch(ctx context.Context, intents []reducer.Intent, _ []reducer.Result) error {
	if err := q.validateClaim(); err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	now := q.now()

	ids := make([]string, len(intents))
	for i, intent := range intents {
		ids[i] = intent.IntentID
	}

	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, now, q.LeaseOwner)
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+3)
		args = append(args, id)
	}

	query := fmt.Sprintf(`
UPDATE fact_work_items
SET status = 'succeeded',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE work_item_id IN (%s)
  AND stage = 'reducer'
  AND lease_owner = $2
  AND status IN ('claimed', 'running')
`, strings.Join(placeholders, ", "))

	if _, err := q.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("batch ack reducer work (%d items): %w", len(intents), err)
	}

	return nil
}

// FailBatch marks multiple claimed reducer work items as failed in a single
// round-trip. Each intent is failed with its corresponding error.
func (q ReducerQueue) FailBatch(ctx context.Context, intents []reducer.Intent, causes []error) error {
	if err := q.validateClaim(); err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	now := q.now()
	for i, intent := range intents {
		cause := causes[i]
		if cause == nil {
			continue
		}
		if err := q.failIntent(ctx, intent, cause); err != nil {
			return fmt.Errorf("batch fail item %d (%s): %w", i, intent.IntentID, err)
		}
	}
	_ = now // used by individual failIntent calls via q.now()
	return nil
}
