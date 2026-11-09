// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// retouchReducerWorkItemQuery records a same-generation re-touch of one
// work item without stealing an active claim (#10411). Succeeded work
// reopens with the shared set clause; pending or retrying work keeps its
// status but advances reopened_at, which is what the claim read derives
// CycleStartedAt from, so freshness gates anchored on the cycle start
// (the advisory-group winners watermark) wait for the later commit, not
// the first enqueue; claimed or running work keeps its lease and carries
// the durable replay bit so the ACK trigger returns it to pending for a
// rerun (the trigger itself stamps reopened_at, advancing the cycle).
const retouchReducerWorkItemQuery = `
UPDATE fact_work_items
SET status = CASE
        WHEN status = 'succeeded' THEN 'pending'
        ELSE status
    END,
    attempt_count = CASE
        WHEN status = 'succeeded' THEN 0
        ELSE attempt_count
    END,
    container_image_identity_v2_authorized_status = CASE
        WHEN status = 'succeeded' AND container_image_identity_v2_required THEN 'pending'
        ELSE container_image_identity_v2_authorized_status
    END,
    container_image_identity_v3_authorized_status = CASE
        WHEN status = 'succeeded' AND container_image_identity_v3_required THEN 'pending'
        ELSE container_image_identity_v3_authorized_status
    END,
    lease_owner = CASE WHEN status = 'succeeded' THEN NULL ELSE lease_owner END,
    claim_until = CASE WHEN status = 'succeeded' THEN NULL ELSE claim_until END,
    visible_at = CASE WHEN status = 'succeeded' THEN $1 ELSE visible_at END,
    next_attempt_at = CASE WHEN status = 'succeeded' THEN NULL ELSE next_attempt_at END,
    updated_at = $1,
    reopened_at = CASE
        WHEN status IN ('succeeded', 'pending', 'retrying') THEN $1
        ELSE reopened_at
    END,
    cross_scope_replay_required = CASE
        WHEN status IN ('claimed', 'running') THEN TRUE
        ELSE cross_scope_replay_required
    END,
    failure_class = CASE WHEN status = 'succeeded' THEN NULL ELSE failure_class END,
    failure_message = CASE WHEN status = 'succeeded' THEN NULL ELSE failure_message END,
    failure_details = CASE WHEN status = 'succeeded' THEN NULL ELSE failure_details END
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND status IN ('pending', 'claimed', 'running', 'retrying', 'succeeded')
`

// RetouchWorkItem records a re-touch of one reducer work item: a
// succeeded row reopens, a pending or retrying row advances its cycle
// start, and a claimed or running row is marked for replay after its
// ACK. The returned boolean reports whether a live row matched.
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

	result, err := q.database.ExecContext(ctx, retouchReducerWorkItemQuery, q.now(), workItemID)
	if err != nil {
		return false, fmt.Errorf("retouch reducer work item: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("retouch reducer work item: rows affected: %w", err)
	}

	return rowsAffected > 0, nil
}
