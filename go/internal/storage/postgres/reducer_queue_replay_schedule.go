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
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Cross-scope replay-debt scheduling (#11104 family, #11615 labels): the
// schedule statements below record replay debt for succeeded rows (reopen
// immediately) and for claimed or running rows (flag for one rerun at ACK).
// Every path stamps cross_scope_replay and counts exactly the rows that
// transitioned; the ACK-trigger conversion later is never double-counted.

// scheduleCrossScopeMaterializationReplayQuery records replay debt without
// stealing an active claim. Succeeded work reopens immediately; claimed or
// running work keeps its exact lease and carries the existing durable
// cross-scope replay bit so the database ACK trigger returns it to pending.
//
// The target CTE locks the row and captures its prior status and replay
// flag, so the caller can count exactly the rows that transitioned: a
// succeeded row returned to pending, or a claimed or running row newly
// flagged for one rerun. Rows already pending or retrying keep their prior
// reason and are not counted; rows already flagged keep theirs too. The CTE
// takes the same FOR NO KEY UPDATE row lock the single-table UPDATE took,
// so lock class and ordering are unchanged (#11615).
var scheduleCrossScopeMaterializationReplayQuery = `
WITH target AS (
    SELECT work_item_id,
           status AS prior_status,
           cross_scope_replay_required AS prior_flag
    FROM fact_work_items
    WHERE work_item_id = $2
      AND stage = 'reducer'
      AND status IN ('pending', 'claimed', 'running', 'retrying', 'succeeded')
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

// workloadMaterializationReplayScheduledQuery resolves the concurrent-insert
// case after enqueue's ON CONFLICT DO NOTHING reports zero affected rows. The
// follow-up statement runs with a fresh Read Committed snapshot, so it can see
// the winner after PostgreSQL finishes conflict arbitration. It returns the
// stable item's status so the caller can tell a superseded item (terminal,
// never revived, #7670) from a dead-lettered one.
const workloadMaterializationReplayScheduledQuery = `
SELECT status
FROM fact_work_items
WHERE work_item_id = $1
  AND stage = 'reducer'
`

const countInFlightReducerWorkByDomainQuery = `
SELECT COUNT(*)
FROM fact_work_items
WHERE stage = 'reducer'
  AND domain = $1
  AND status NOT IN ('succeeded', 'dead_letter')
`

const (
	workloadMaterializationReplayReason = "deployment mapping resolved stronger evidence"
	// crossplaneSatisfiedByRedriveReplayReason tags a re-drive-triggered
	// SATISFIED_BY replay (issue #5476) distinctly from the original
	// projector-triggered enqueue, so a completion log or dead-letter can
	// tell the two apart.
	crossplaneSatisfiedByRedriveReplayReason = "cross-scope crossplane xrd redrive"
)

// ReopenSucceeded moves one succeeded reducer work item back to pending so it
// can be replayed through the normal reducer claim path. reason declares why
// the row reopens, from the closed #11615 vocabulary; the compiler forces
// every caller to name its own (a shared constant could not hardcode one
// reason for all call sites). The returned boolean reports whether a
// succeeded row was actually transitioned: callers accumulate the transitioned
// count and emit the per-reason counter after their own commit point, so a
// rolled-back transaction never counts.
func (q ReducerQueue) ReopenSucceeded(
	ctx context.Context,
	workItemID string,
	reason telemetry.RequeueReason,
) (bool, error) {
	if err := q.validateDB(); err != nil {
		return false, err
	}
	if strings.TrimSpace(workItemID) == "" {
		return false, errors.New("reducer work item id is required")
	}
	if err := reason.Validate(); err != nil {
		return false, fmt.Errorf("reopen succeeded reducer work: %w", err)
	}

	result, err := q.database.ExecContext(ctx, reopenSucceededReducerWork(reason), q.now(), workItemID)
	if err != nil {
		return false, fmt.Errorf("reopen succeeded reducer work: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("reopen succeeded reducer work: rows affected: %w", err)
	}

	return rowsAffected > 0, nil
}

// ReplayDomain moves succeeded reducer work items for one scope-generation
// domain back to pending so they can be replayed through the normal claim
// path. reason declares why the rows reopen, from the closed #11615
// vocabulary. The returned boolean reports whether any succeeded row was
// actually transitioned; callers emit the per-reason counter after their own
// commit point.
func (q ReducerQueue) ReplayDomain(
	ctx context.Context,
	scopeID string,
	generationID string,
	domain reducercontract.Domain,
	reason telemetry.RequeueReason,
) (bool, error) {
	if err := q.validateDB(); err != nil {
		return false, err
	}
	if strings.TrimSpace(scopeID) == "" {
		return false, errors.New("reducer replay scope id is required")
	}
	if strings.TrimSpace(generationID) == "" {
		return false, errors.New("reducer replay generation id is required")
	}
	if err := domain.Validate(); err != nil {
		return false, fmt.Errorf("reducer replay domain: %w", err)
	}
	if err := reason.Validate(); err != nil {
		return false, fmt.Errorf("replay succeeded reducer domain: %w", err)
	}

	result, err := q.database.ExecContext(
		ctx,
		replaySucceededReducerDomain(reason),
		q.now(),
		scopeID,
		generationID,
		string(domain),
	)
	if err != nil {
		return false, fmt.Errorf("replay succeeded reducer domain: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("replay succeeded reducer domain: rows affected: %w", err)
	}

	return rowsAffected > 0, nil
}

// ReplayWorkloadMaterialization replays the canonical workload materialization
// intent(s) for one scope generation after stronger deployment evidence lands.
// It returns false only when neither this caller nor a concurrent caller has
// scheduled replayable work, including when the stable work-item identity
// already names a dead-lettered or superseded row. Use
// [ReducerQueue.ReplayWorkloadMaterializationOutcome] to tell those apart.
func (q ReducerQueue) ReplayWorkloadMaterialization(
	ctx context.Context,
	scopeID string,
	generationID string,
	entityKey string,
) (bool, error) {
	outcome, err := q.ReplayWorkloadMaterializationOutcome(ctx, scopeID, generationID, entityKey)
	if err != nil {
		return false, err
	}
	return outcome == reducer.WorkloadMaterializationReplayScheduled, nil
}

// ReplayWorkloadMaterializationOutcome schedules the same replay as
// [ReducerQueue.ReplayWorkloadMaterialization] and reports the closed outcome.
// A terminally superseded stable item reports
// [reducer.WorkloadMaterializationReplaySuperseded]: no UPDATE matches it and
// the enqueue conflicts on the stable identity, so the queue will never run it.
// The store does not say whether that is owed. The repo-dependency runner
// decides: it skips only a retired generation and fails closed on the active
// one (#7670). Every other unscheduled state, including a dead-lettered item or
// a missing row after an enqueue conflict, reports
// [reducer.WorkloadMaterializationReplayNotScheduled].
func (q ReducerQueue) ReplayWorkloadMaterializationOutcome(
	ctx context.Context,
	scopeID string,
	generationID string,
	entityKey string,
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

	intent := runtime.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainWorkloadMaterialization,
		EntityKey:    entityKey,
		Reason:       workloadMaterializationReplayReason,
		SourceSystem: "reducer",
	}
	workItemID := reducerWorkItemID(intent)

	rows, err := q.database.QueryContext(ctx, scheduleCrossScopeMaterializationReplayQuery, q.now(), workItemID)
	if err != nil {
		return reducer.WorkloadMaterializationReplayNotScheduled,
			fmt.Errorf("schedule workload materialization replay: %w", err)
	}
	transitioned, err := countScheduledReplay(rows, "schedule workload materialization replay")
	if err != nil {
		return reducer.WorkloadMaterializationReplayNotScheduled, err
	}
	if transitioned.matched {
		// The statement always runs autocommit here, so writer-level
		// emission is post-commit by construction.
		telemetry.RecordReducerRequeue(ctx, q.Instruments, telemetry.RequeueReasonCrossScopeReplay, transitioned.requeues)
		return reducer.WorkloadMaterializationReplayScheduled, nil
	}
	return q.enqueueWorkloadMaterializationReplayTail(ctx, intent, workItemID)
}

// enqueueWorkloadMaterializationReplayTail enqueues a missing replay intent
// and maps the post-conflict stable status to the closed replay outcome. It
// is shared by the unfenced and the digest-fenced replay so both agree on
// what an absent or terminal row means.
func (q ReducerQueue) enqueueWorkloadMaterializationReplayTail(
	ctx context.Context,
	intent runtime.ReducerIntent,
	workItemID string,
) (reducer.WorkloadMaterializationReplayOutcome, error) {
	inserted, err := q.enqueueReducerBatch(ctx, []runtime.ReducerIntent{intent}, q.now())
	if err != nil {
		return reducer.WorkloadMaterializationReplayNotScheduled,
			fmt.Errorf("schedule workload materialization replay: %w", err)
	}
	if inserted > 0 {
		return reducer.WorkloadMaterializationReplayScheduled, nil
	}

	return q.workloadMaterializationReplayOutcome(ctx, workItemID)
}

// workloadMaterializationReplayOutcome maps the stable work item's status after
// an enqueue conflict to the closed replay outcome.
func (q ReducerQueue) workloadMaterializationReplayOutcome(
	ctx context.Context,
	workItemID string,
) (reducer.WorkloadMaterializationReplayOutcome, error) {
	rows, err := q.database.QueryContext(ctx, workloadMaterializationReplayScheduledQuery, workItemID)
	if err != nil {
		return reducer.WorkloadMaterializationReplayNotScheduled,
			fmt.Errorf("check workload materialization replay after enqueue conflict: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return reducer.WorkloadMaterializationReplayNotScheduled,
				fmt.Errorf("iterate workload materialization replay after enqueue conflict: %w", err)
		}
		return reducer.WorkloadMaterializationReplayNotScheduled, nil
	}
	var status string
	if err := rows.Scan(&status); err != nil {
		return reducer.WorkloadMaterializationReplayNotScheduled,
			fmt.Errorf("scan workload materialization replay after enqueue conflict: %w", err)
	}
	if err := rows.Err(); err != nil {
		return reducer.WorkloadMaterializationReplayNotScheduled,
			fmt.Errorf("iterate workload materialization replay after enqueue conflict: %w", err)
	}
	switch status {
	case "pending", "claimed", "running", "retrying", "succeeded":
		return reducer.WorkloadMaterializationReplayScheduled, nil
	case "superseded":
		return reducer.WorkloadMaterializationReplaySuperseded, nil
	default:
		return reducer.WorkloadMaterializationReplayNotScheduled, nil
	}
}

// ReplayCrossplaneSatisfiedByMaterialization schedules one target Claim
// scope's SATISFIED_BY materialization after cross-scope XRD activation.
// It uses the same stable identity as the projector trigger. An active claim
// keeps its lease and receives durable replay debt, which the ACK trigger
// returns to pending. False means a terminal or missing identity could not be
// scheduled; the caller must leave its sweep incomplete for recovery.
func (q ReducerQueue) ReplayCrossplaneSatisfiedByMaterialization(
	ctx context.Context,
	targetScopeID string,
	targetGenerationID string,
) (bool, error) {
	if err := q.validateEnqueue(); err != nil {
		return false, err
	}
	if strings.TrimSpace(targetScopeID) == "" {
		return false, errors.New("crossplane satisfied-by redrive target scope id is required")
	}
	if strings.TrimSpace(targetGenerationID) == "" {
		return false, errors.New("crossplane satisfied-by redrive target generation id is required")
	}

	intent := runtime.ReducerIntent{
		ScopeID:      targetScopeID,
		GenerationID: targetGenerationID,
		Domain:       reducer.DomainCrossplaneSatisfiedByMaterialization,
		EntityKey:    "crossplane_satisfied_by_materialization:" + targetScopeID,
		Reason:       crossplaneSatisfiedByRedriveReplayReason,
		SourceSystem: "reducer",
	}
	workItemID := reducerWorkItemID(intent)

	scheduled, err := q.scheduleCrossScopeMaterializationReplay(ctx, workItemID)
	if err != nil {
		return false, fmt.Errorf("schedule crossplane satisfied-by redrive replay: %w", err)
	}
	if scheduled {
		return true, nil
	}
	inserted, err := q.enqueueReducerBatch(ctx, []runtime.ReducerIntent{intent}, q.now())
	if err != nil {
		return false, fmt.Errorf("schedule crossplane satisfied-by redrive replay: %w", err)
	}
	if inserted > 0 {
		return true, nil
	}

	// ON CONFLICT DO NOTHING can lose to a concurrent insert that was not
	// visible to the first UPDATE. A fresh statement must record replay debt
	// on the winning row, not merely read its status and assume it is owed.
	scheduled, err = q.scheduleCrossScopeMaterializationReplay(ctx, workItemID)
	if err != nil {
		return false, fmt.Errorf("schedule crossplane satisfied-by redrive replay after conflict: %w", err)
	}
	return scheduled, nil
}

func (q ReducerQueue) scheduleCrossScopeMaterializationReplay(ctx context.Context, workItemID string) (bool, error) {
	rows, err := q.database.QueryContext(ctx, scheduleCrossScopeMaterializationReplayQuery, q.now(), workItemID)
	if err != nil {
		return false, err
	}
	transitioned, err := countScheduledReplay(rows, "schedule cross-scope materialization replay")
	if err != nil {
		return false, err
	}
	// The statement always runs autocommit here, so writer-level emission
	// is post-commit by construction.
	telemetry.RecordReducerRequeue(ctx, q.Instruments, telemetry.RequeueReasonCrossScopeReplay, transitioned.requeues)
	return transitioned.matched, nil
}
