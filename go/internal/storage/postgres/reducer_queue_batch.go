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
      AND ($2 = '' OR domain = $2)
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
            AND ($2 = '' OR same.domain = $2)
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
		q.claimDomainFilter(),
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
