// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const ackReducerWorkQuery = `
UPDATE fact_work_items
SET status = 'succeeded', lease_owner = NULL, claim_until = NULL,
    provenance_edge_identity_upgrade_required = FALSE,
    visible_at = NULL, updated_at = $1,
    failure_class = NULL, failure_message = NULL, failure_details = NULL
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND lease_owner = $3
  AND status IN ('claimed', 'running')
  AND claim_until > clock_timestamp()
  AND last_attempt_at = $4
`

const ackContainerImageIdentityReducerWorkQuery = `
WITH acknowledged AS MATERIALIZED (
    UPDATE fact_work_items AS work
    SET status = 'succeeded',
        provenance_edge_identity_upgrade_required = FALSE,
        cross_scope_completion_ack_epoch = cross_scope_completion_ack_epoch + 1,
        container_image_identity_v2_authorized_status = CASE
            WHEN container_image_identity_v2_required THEN 'succeeded' ELSE '' END,
        container_image_identity_v3_authorized_status = CASE
            WHEN container_image_identity_v3_required THEN 'succeeded' ELSE '' END,
        lease_owner = NULL, claim_until = NULL, visible_at = NULL,
        updated_at = $1, failure_class = NULL,
        failure_message = NULL, failure_details = NULL
    WHERE work.work_item_id = $2
      AND stage = 'reducer'
      AND domain = 'container_image_identity'
      AND lease_owner = $3
      AND status IN ('claimed', 'running')
      AND container_image_identity_claim_epoch = $4
      AND claim_until > clock_timestamp()
      AND last_attempt_at = $5
    RETURNING work.work_item_id, work.status
), emission_clock AS MATERIALIZED (
    SELECT clock_timestamp() AS emitted_at
), emitted AS (
    INSERT INTO cross_scope_completion_events (
        producer_domain, producer_item_count, status,
        visible_at, created_at, updated_at
    )
    SELECT 'container_image_identity', count(*)::BIGINT, 'pending',
           emission_clock.emitted_at + INTERVAL '250 milliseconds',
           emission_clock.emitted_at, emission_clock.emitted_at
    FROM acknowledged CROSS JOIN emission_clock
    WHERE acknowledged.status = 'succeeded'
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

const ackCICDRunCorrelationReducerWorkQuery = `
WITH acknowledged AS MATERIALIZED (
    UPDATE fact_work_items AS work
    SET status = 'succeeded',
        cross_scope_completion_ack_epoch = cross_scope_completion_ack_epoch + 1,
        lease_owner = NULL, claim_until = NULL, visible_at = NULL,
        updated_at = $1, failure_class = NULL,
        failure_message = NULL, failure_details = NULL
    WHERE work.work_item_id = $2
      AND stage = 'reducer'
      AND domain = 'ci_cd_run_correlation'
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
    SELECT 'ci_cd_run_correlation', count(*)::BIGINT, 'pending',
           emission_clock.emitted_at + INTERVAL '250 milliseconds',
           emission_clock.emitted_at, emission_clock.emitted_at
    FROM acknowledged CROSS JOIN emission_clock
    WHERE acknowledged.status = 'succeeded'
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

const scheduleWorkloadMaterializationFencedReplayQuery = `
UPDATE fact_work_items
SET status = CASE
        WHEN status = 'succeeded' THEN 'pending'
        ELSE status
    END,
    attempt_count = CASE WHEN status = 'succeeded' THEN 0 ELSE attempt_count END,
    payload = jsonb_set(
        jsonb_set(payload, '{repo_dependency_readiness_fence}', to_jsonb($3::text), TRUE),
        '{repo_dependency_readiness_repo_id}', to_jsonb($4::text), TRUE
    ),
    lease_owner = CASE WHEN status = 'succeeded' THEN NULL ELSE lease_owner END,
    claim_until = CASE WHEN status = 'succeeded' THEN NULL ELSE claim_until END,
    visible_at = CASE WHEN status = 'succeeded' THEN $1 ELSE visible_at END,
    next_attempt_at = CASE WHEN status = 'succeeded' THEN NULL ELSE next_attempt_at END,
    updated_at = CASE
        WHEN status = 'succeeded'
          OR COALESCE(payload->>'repo_dependency_readiness_fence', '') <> $3
            THEN $1
        ELSE updated_at
    END,
    reopened_at = CASE WHEN status = 'succeeded' THEN $1 ELSE reopened_at END,
    cross_scope_replay_required = CASE
        WHEN status IN ('claimed', 'running')
          AND COALESCE(payload->>'repo_dependency_readiness_fence', '') <> $3
            THEN TRUE
        ELSE cross_scope_replay_required
    END,
    failure_class = CASE WHEN status = 'succeeded' THEN NULL ELSE failure_class END,
    failure_message = CASE WHEN status = 'succeeded' THEN NULL ELSE failure_message END,
    failure_details = CASE WHEN status = 'succeeded' THEN NULL ELSE failure_details END
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND status IN ('pending', 'claimed', 'running', 'retrying', 'succeeded')
`

// ReplayWorkloadMaterializationForFence schedules workload materialization
// carrying the deterministic RUNS_ON input fence that its successful handler
// pass must publish. A concurrent first insert is followed by one update retry
// so the winning row cannot retain an older token.
func (q ReducerQueue) ReplayWorkloadMaterializationForFence(
	ctx context.Context,
	scopeID string,
	generationID string,
	entityKey string,
	repoID string,
	fence string,
) (bool, error) {
	if err := q.validateEnqueue(); err != nil {
		return false, err
	}
	if strings.TrimSpace(entityKey) == "" {
		return false, errors.New("workload materialization fenced replay entity key is required")
	}
	if strings.TrimSpace(repoID) == "" {
		return false, errors.New("workload materialization fenced replay repository id is required")
	}
	if strings.TrimSpace(fence) == "" {
		return false, errors.New("workload materialization fenced replay token is required")
	}

	intent := runtime.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainWorkloadMaterialization,
		EntityKey:    entityKey,
		Reason:       workloadMaterializationReplayReason,
		SourceSystem: "reducer",
		Payload: map[string]any{
			reducer.RepoDependencyReadinessFencePayloadKey:  fence,
			reducer.RepoDependencyReadinessRepoIDPayloadKey: repoID,
		},
	}
	workItemID := reducerWorkItemID(intent)
	matched, err := q.scheduleWorkloadMaterializationFencedReplay(ctx, workItemID, fence, repoID)
	if err != nil || matched {
		return matched, err
	}
	inserted, err := q.enqueueReducerBatch(ctx, []runtime.ReducerIntent{intent}, q.now())
	if err != nil {
		return false, fmt.Errorf("schedule fenced workload materialization replay: %w", err)
	}
	if inserted > 0 {
		return true, nil
	}

	return q.scheduleWorkloadMaterializationFencedReplay(ctx, workItemID, fence, repoID)
}

func (q ReducerQueue) scheduleWorkloadMaterializationFencedReplay(
	ctx context.Context,
	workItemID string,
	fence string,
	repoID string,
) (bool, error) {
	result, err := q.database.ExecContext(
		ctx,
		scheduleWorkloadMaterializationFencedReplayQuery,
		q.now(),
		workItemID,
		fence,
		repoID,
	)
	if err != nil {
		return false, fmt.Errorf("schedule fenced workload materialization replay: %w", err)
	}
	matched, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("schedule fenced workload materialization replay: rows affected: %w", err)
	}
	return matched > 0, nil
}
