// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// retouchReducerWorkItemQuery records a same-generation re-touch of one
// work item without stealing an active claim (#10411). Succeeded work
// reopens with the shared set clause; pending or retrying work keeps its
// status and its updated_at -- so the fair-rank claim order keeps the
// row's queue position instead of requeueing it behind the backlog
// (#10972) -- but still advances reopened_at, which is what the claim
// read derives CycleStartedAt from, so freshness gates anchored on the
// cycle start (the advisory-group winners watermark) wait for the later
// commit, not the first enqueue; claimed or running work keeps its lease,
// advances reopened_at in place (so a Fail to retrying -- whose trigger
// only clears the replay bit -- still retries against the later bound),
// and carries the durable replay bit so an ACK-success returns it to
// pending for a rerun instead of succeeding.
//
// Terminal work (#10566) reopens with the succeeded shape: a touch landing
// on a failed or dead-letter slot is new evidence, not the same poison, so
// the slot returns to pending with zeroed attempts, cleared failure
// evidence, visible_at advanced to the touch (promptly claimable, exactly
// like a succeeded reopen), no scheduled retry, and the bound advanced to
// the touch. Without this the touch is silently dropped and a single
// dead-letter wedges its group until an operator replays it. The only
// production caller is the advisory-group projection enqueuer, whose slots
// cycle pending to claimed to succeeded and back to pending on retouch;
// the widened set only admits rows that previously saw a no-match retouch.
// retouchReducerWorkItemQuery records a same-generation re-touch with a
// target pre-image CTE: the CTE locks the row and captures its prior status
// and replay flag, so RetouchWorkItem counts exactly the touches that
// transitioned (a finished row reopened to pending, or a claimed or running
// row newly flagged for replay debt). Rows already pending or retrying keep
// their prior reason and are not counted. Unlike a waiting wake, that touch
// exits no parked sub-state and advances neither visible_at nor updated_at,
// so the unconditional reopened_at = $1 only moves the already-scheduled
// run's cycle bound. The CTE takes the same FOR NO KEY
// UPDATE row lock the single-table UPDATE took (#11615).
var retouchReducerWorkItemQuery = `
WITH target AS (
    SELECT work_item_id,
           status AS prior_status,
           cross_scope_replay_required AS prior_flag
    FROM fact_work_items
    WHERE work_item_id = $2
      AND stage = 'reducer'
      AND status IN ('pending', 'claimed', 'running', 'retrying', 'succeeded', 'failed', 'dead_letter')
    FOR NO KEY UPDATE
)
UPDATE fact_work_items AS work
SET status = CASE
        WHEN work.status IN ('succeeded', 'failed', 'dead_letter') THEN 'pending'
        ELSE work.status
    END,
    attempt_count = CASE
        WHEN work.status IN ('succeeded', 'failed', 'dead_letter') THEN 0
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
    lease_owner = CASE WHEN status IN ('succeeded', 'failed', 'dead_letter') THEN NULL ELSE lease_owner END,
    claim_until = CASE WHEN status IN ('succeeded', 'failed', 'dead_letter') THEN NULL ELSE claim_until END,
    visible_at = CASE WHEN status IN ('succeeded', 'failed', 'dead_letter') THEN $1 ELSE visible_at END,
    next_attempt_at = CASE WHEN status IN ('succeeded', 'failed', 'dead_letter') THEN NULL ELSE next_attempt_at END,
    updated_at = CASE WHEN status IN ('succeeded', 'failed', 'dead_letter') THEN $1 ELSE updated_at END,
    reopened_at = $1,
    last_requeue_reason = ` + queuestore.RequeueReasonScheduleValue(
	telemetry.RequeueReasonRetouchNewEvidence.SQLLiteral(), "work", "('succeeded', 'failed', 'dead_letter')") + `,
    cross_scope_replay_required = CASE
        WHEN work.status IN ('claimed', 'running') THEN TRUE
        ELSE FALSE
    END,
    failure_class = CASE WHEN work.status IN ('succeeded', 'failed', 'dead_letter') THEN NULL ELSE work.failure_class END,
    failure_message = CASE WHEN work.status IN ('succeeded', 'failed', 'dead_letter') THEN NULL ELSE work.failure_message END,
    failure_details = CASE WHEN work.status IN ('succeeded', 'failed', 'dead_letter') THEN NULL ELSE work.failure_details END
FROM target
WHERE work.work_item_id = target.work_item_id
RETURNING (target.prior_status IN ('succeeded', 'failed', 'dead_letter')
    OR (target.prior_status IN ('claimed', 'running')
        AND NOT target.prior_flag)) AS requeued
`

// RetouchWorkItem records a re-touch of one reducer work item: a
// succeeded, failed, or dead-letter row reopens to pending with the
// succeeded shape, a pending or retrying row advances its cycle start,
// and a claimed or running row is marked for replay after its ACK. The
// returned boolean reports whether a live row matched; the per-reason
// counter counts only the touches that transitioned (finished rows reopened,
// claimed or running rows newly flagged), emitted here because the statement
// always runs autocommit.
func (q ReducerQueue) RetouchWorkItem(
	ctx context.Context,
	workItemID string,
) (bool, error) {
	if err := q.validateDB(); err != nil {
		return false, err
	}
	if strings.TrimSpace(workItemID) == "" {
		return false, errors.New("reducer work item id is required")
	}

	rows, err := q.database.QueryContext(ctx, retouchReducerWorkItemQuery, q.now(), workItemID)
	if err != nil {
		return false, fmt.Errorf("retouch reducer work item: %w", err)
	}
	transitioned, err := countScheduledReplay(rows, "retouch reducer work item")
	if err != nil {
		return false, err
	}
	telemetry.RecordReducerRequeue(ctx, q.Instruments, telemetry.RequeueReasonRetouchNewEvidence, transitioned.requeues)

	return transitioned.matched, nil
}
