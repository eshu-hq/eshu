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
	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// deploymentEvidenceDigestPayloadKey records the resolved-set digest of the
// deployment-evidence pass that last replayed a workload materialization
// item. A rerun carrying the same digest leaves a finished item alone; a
// changed digest reopens it. It mirrors the repo_dependency_readiness_fence
// payload key on the RUNS_ON path. The schedule statement below spells the
// key literally (like the fenced twin) so the #11615 writer guard sees the
// UPDATE head and the reason arm in one fragment; the quiet probe here keeps
// the constant, and the live tests pin both sides to the same key.
const deploymentEvidenceDigestPayloadKey = "deployment_evidence_digest"

// scheduleWorkloadMaterializationDigestReplayQuery is the digest-fenced twin
// of scheduleCrossScopeMaterializationReplayQuery for the deployment-evidence
// replay path. It stamps the incoming resolved-set digest on every row it
// touches, but it leaves a finished row completely alone -- status,
// updated_at, and reopened_at all keep their values, so the row holds its
// fair-queue position and its winners-watermark bound -- when the row already
// records the same non-empty digest. A finished row with no recorded digest,
// or a call with no digest, reopens exactly like the unfenced statement
// (fail open: the first fenced pass still replays, and nothing owed is
// skipped). The claimed/running arm is unchanged: an in-flight row keeps its
// lease and carries replay debt for its ACK, because it may not have consumed
// the evidence yet. The target CTE locks the row and captures its prior
// status and replay flag, applying the digest gate in the CTE WHERE so a
// quiet rerun selects nothing, locks nothing, and stamps nothing; the
// UPDATE joins the target and the RETURNING boolean counts exactly the
// transitions. Same single-row primary-key update as the unfenced
// statement: no new lock order, and concurrent reruns converge on the same
// end state (#11615).
var scheduleWorkloadMaterializationDigestReplayQuery = `
WITH target AS (
    SELECT work_item_id,
           status AS prior_status,
           cross_scope_replay_required AS prior_flag
    FROM fact_work_items
    WHERE work_item_id = $2
      AND stage = 'reducer'
      AND status IN ('pending', 'claimed', 'running', 'retrying', 'succeeded')
      AND (
        status <> 'succeeded'
        OR COALESCE(payload->>'deployment_evidence_digest', '') <> $3
        OR $3 = ''
      )
    FOR NO KEY UPDATE
)
UPDATE fact_work_items AS work
SET status = CASE
        WHEN work.status = 'succeeded' THEN 'pending'
        ELSE work.status
    END,
    attempt_count = CASE
        WHEN work.status = 'succeeded' THEN 0
        ELSE work.attempt_count
    END,
    container_image_identity_v2_authorized_status = CASE
        WHEN work.status = 'succeeded' AND work.container_image_identity_v2_required THEN 'pending'
        ELSE work.container_image_identity_v2_authorized_status
    END,
    container_image_identity_v3_authorized_status = CASE
        WHEN work.status = 'succeeded' AND work.container_image_identity_v3_required THEN 'pending'
        ELSE work.container_image_identity_v3_authorized_status
    END,
    payload = jsonb_set(work.payload, '{deployment_evidence_digest}', to_jsonb($3::text), TRUE),
    lease_owner = CASE WHEN work.status = 'succeeded' THEN NULL ELSE work.lease_owner END,
    claim_until = CASE WHEN work.status = 'succeeded' THEN NULL ELSE work.claim_until END,
    visible_at = CASE WHEN work.status = 'succeeded' THEN $1 ELSE work.visible_at END,
    next_attempt_at = CASE WHEN work.status = 'succeeded' THEN NULL ELSE work.next_attempt_at END,
    updated_at = CASE
        WHEN work.status = 'succeeded' OR work.status IN ('claimed', 'running') THEN $1
        ELSE work.updated_at
    END,
    reopened_at = CASE WHEN work.status = 'succeeded' THEN $1 ELSE work.reopened_at END,
    last_requeue_reason = ` + queuestore.RequeueReasonScheduleValue(
	telemetry.RequeueReasonCrossScopeReplay.SQLLiteral(), "work", "('succeeded')") + `,
    cross_scope_replay_required = CASE
        WHEN work.status IN ('claimed', 'running') THEN TRUE
        ELSE FALSE
    END,
    failure_class = CASE WHEN work.status = 'succeeded' THEN NULL ELSE work.failure_class END,
    failure_message = CASE WHEN work.status = 'succeeded' THEN NULL ELSE work.failure_message END,
    failure_details = CASE WHEN work.status = 'succeeded' THEN NULL ELSE work.failure_details END
FROM target
WHERE work.work_item_id = target.work_item_id
RETURNING (target.prior_status = 'succeeded'
    OR (target.prior_status IN ('claimed', 'running')
        AND NOT target.prior_flag)) AS requeued
`

// ReplayWorkloadMaterializationForDigest replays the canonical workload
// materialization intent(s) for one scope generation after stronger deployment
// evidence lands, carrying the resolved-set digest of the pass that triggered
// it. A finished item that already records the same digest is left alone, so
// a rerun over an unchanged resolved set stays quiet; a changed digest
// reopens it. It returns false only when the rerun stayed quiet or when
// neither this caller nor a concurrent caller has scheduled replayable work.
// Use [ReducerQueue.ReplayWorkloadMaterializationForDigestOutcome] to tell a
// quiet rerun apart from the other unscheduled states.
func (q ReducerQueue) ReplayWorkloadMaterializationForDigest(
	ctx context.Context,
	scopeID string,
	generationID string,
	entityKey string,
	digest string,
) (bool, error) {
	outcome, err := q.ReplayWorkloadMaterializationForDigestOutcome(ctx, scopeID, generationID, entityKey, digest)
	if err != nil {
		return false, err
	}
	return outcome == reducer.WorkloadMaterializationReplayScheduled, nil
}

// ReplayWorkloadMaterializationForDigestOutcome schedules the same
// digest-fenced replay as [ReducerQueue.ReplayWorkloadMaterializationForDigest]
// and reports the closed outcome. A finished item that already records the
// incoming digest reports
// [reducer.WorkloadMaterializationReplayNotScheduled]: nothing was reopened.
// An empty digest delegates to
// [ReducerQueue.ReplayWorkloadMaterializationOutcome] (fail open), and every
// other unscheduled state keeps that method's meaning.
func (q ReducerQueue) ReplayWorkloadMaterializationForDigestOutcome(
	ctx context.Context,
	scopeID string,
	generationID string,
	entityKey string,
	digest string,
) (reducer.WorkloadMaterializationReplayOutcome, error) {
	// Replay records durable scheduling: it reopens succeeded work, marks an
	// active claim for replay after ACK, accepts already-pending work, or
	// enqueues a missing intent. It never steals or rewrites an active lease.
	if err := q.validateEnqueue(); err != nil {
		return reducer.WorkloadMaterializationReplayNotScheduled, err
	}
	if strings.TrimSpace(entityKey) == "" {
		return reducer.WorkloadMaterializationReplayNotScheduled,
			errors.New("workload materialization replay entity key is required")
	}
	if strings.TrimSpace(digest) == "" {
		return q.ReplayWorkloadMaterializationOutcome(ctx, scopeID, generationID, entityKey)
	}

	intent := runtime.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainWorkloadMaterialization,
		EntityKey:    entityKey,
		Reason:       workloadMaterializationReplayReason,
		SourceSystem: "reducer",
	}
	workItemID := reducerWorkItemID(intent)

	rows, err := q.database.QueryContext(ctx, scheduleWorkloadMaterializationDigestReplayQuery, q.now(), workItemID, digest)
	if err != nil {
		return reducer.WorkloadMaterializationReplayNotScheduled,
			fmt.Errorf("schedule workload materialization digest replay: %w", err)
	}
	transitioned, err := countScheduledReplay(rows, "schedule workload materialization digest replay")
	if err != nil {
		return reducer.WorkloadMaterializationReplayNotScheduled, err
	}
	if transitioned.matched {
		// The statement always runs autocommit here, so writer-level
		// emission is post-commit by construction.
		telemetry.RecordReducerRequeue(ctx, q.Instruments, telemetry.RequeueReasonCrossScopeReplay, transitioned.requeues)
		return reducer.WorkloadMaterializationReplayScheduled, nil
	}
	// No row moved: the item is missing (or terminal) or a finished item
	// already records this digest, in which case the rerun stays quiet.
	quiet, err := q.workloadMaterializationDigestQuiet(ctx, workItemID, digest)
	if err != nil {
		return reducer.WorkloadMaterializationReplayNotScheduled, err
	}
	if quiet {
		return reducer.WorkloadMaterializationReplayNotScheduled, nil
	}
	return q.enqueueWorkloadMaterializationReplayTail(ctx, intent, workItemID)
}

// workloadMaterializationDigestQuiet reports whether a finished work item
// already records the incoming resolved-set digest. The digest-fenced update
// skips that row entirely, so this probe runs only when the update matched
// nothing; it reads the row the update just saw under a fresh snapshot.
func (q ReducerQueue) workloadMaterializationDigestQuiet(
	ctx context.Context,
	workItemID string,
	digest string,
) (bool, error) {
	rows, err := q.database.QueryContext(ctx,
		`SELECT status, COALESCE(payload->>'`+deploymentEvidenceDigestPayloadKey+`', '')
		 FROM fact_work_items
		 WHERE work_item_id = $1 AND stage = 'reducer'`,
		workItemID,
	)
	if err != nil {
		return false, fmt.Errorf("read workload materialization digest fence: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("iterate workload materialization digest fence: %w", err)
		}
		return false, nil
	}
	var status, stored string
	if err := rows.Scan(&status, &stored); err != nil {
		return false, fmt.Errorf("scan workload materialization digest fence: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate workload materialization digest fence: %w", err)
	}
	return status == "succeeded" && stored == digest, nil
}
