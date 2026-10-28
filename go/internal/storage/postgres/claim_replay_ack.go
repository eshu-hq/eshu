// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Q1 replay-ack alignment (#9428, ruling 6476206689 SATELLITE HOLDS). An
// identical replay of an already-applied terminal claim mutation returns nil;
// every other zero-row shape stays ErrWorkflowClaimRejected. Heartbeat never
// acks post-terminal.

// claimMutationOp identifies which claim-plane operation a mutation executes.
// The zero-row replay classifier needs it: an identical replay acks only when
// the stored epoch already rests in this op's terminal status (#9428).
type claimMutationOp string

const (
	claimOpHeartbeat     claimMutationOp = "heartbeat"
	claimOpComplete      claimMutationOp = "complete"
	claimOpRelease       claimMutationOp = "release"
	claimOpFailRetryable claimMutationOp = "fail_retryable"
	claimOpFailTerminal  claimMutationOp = "fail_terminal"
)

// terminalStatuses reports the (workflow_claims.status,
// workflow_work_items.status) pair the op writes on success. It returns false
// for heartbeat: a heartbeat writes no terminal state, so it can never
// distinguish an identical replay from a stale one and never acks.
func (op claimMutationOp) terminalStatuses() (workflow.ClaimStatus, workflow.WorkItemStatus, bool) {
	switch op {
	case claimOpComplete:
		return workflow.ClaimStatusCompleted, workflow.WorkItemStatusCompleted, true
	case claimOpRelease:
		return workflow.ClaimStatusReleased, workflow.WorkItemStatusPending, true
	case claimOpFailRetryable:
		return workflow.ClaimStatusFailedRetryable, workflow.WorkItemStatusPending, true
	case claimOpFailTerminal:
		return workflow.ClaimStatusFailedTerminal, workflow.WorkItemStatusFailedTerminal, true
	default:
		return "", "", false
	}
}

// Replay-ack classification reads. Both are single-row primary-key lookups
// that deliberately bypass the tenant-grant join in the mutation statements:
// the original mutation already passed the grant check when it committed, and
// a replay must answer from the stored epoch alone — grant visibility at
// replay time must neither ack a foreign epoch nor hide a committed one.
const (
	replayAckClaimQuery = `SELECT status, fencing_token, owner_id, work_item_id FROM workflow_claims WHERE claim_id = $1`
	replayAckItemQuery  = `SELECT status, current_fencing_token FROM workflow_work_items WHERE work_item_id = $1`
)

// classifyClaimMutationResult maps a mutation result to the claim contract: a
// matched row is success, and a zero-row terminal op falls through to the
// replay classifier. A zero-row heartbeat is rejected outright.
func (s *WorkflowControlStore) classifyClaimMutationResult(
	ctx context.Context,
	op claimMutationOp,
	mutation workflow.ClaimMutation,
	result sql.Result,
) error {
	if result == nil {
		return fmt.Errorf("workflow claim mutation result is required")
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read workflow claim mutation rows affected: %w", err)
	}
	if rowsAffected != 0 {
		return nil
	}
	return s.classifyZeroRowClaimMutation(ctx, op, mutation)
}

// classifyZeroRowClaimMutation returns nil when the zero-row mutation is an
// identical replay of an already-applied terminal epoch: the claim row exists
// with matching fencing and owner in the op's terminal status, the claim row
// is bound to the mutation's work item, and the work item still carries the
// mutation's fencing token in the op's terminal status. Everything else stays
// ErrWorkflowClaimRejected.
//
// False-ack impossibility: nil requires the claim row to already rest in the
// op's terminal status, which only the op's own mutation statement writes, and
// that statement writes the claim and item rows atomically in one statement —
// a committed terminal claim always pairs with its terminal item state, so the
// two reads cannot straddle a torn commit. The stored claim work_item_id must
// equal the mutation's work item: every mutation statement joins
// claim.work_item_id to item.work_item_id, so a crossed claim/item pair
// matches zero rows and only an identical-pair replay may ack. A successor
// epoch can only move the item fencing forward (claim issuance bumps the token
// monotonically), which breaks the item-fencing equality, and it leaves the
// claim row in its own terminal status, which breaks the status equality for
// any other op. A concurrent identical replay racing this read already
// committed the same terminal state, so acking it double-applies nothing: the
// mutation matched zero rows precisely because the state is already there.
// Expired epochs never match an op terminal status, and heartbeats never reach
// this classifier.
func (s *WorkflowControlStore) classifyZeroRowClaimMutation(
	ctx context.Context,
	op claimMutationOp,
	mutation workflow.ClaimMutation,
) error {
	wantClaimStatus, wantItemStatus, ok := op.terminalStatuses()
	if !ok {
		return ErrWorkflowClaimRejected
	}
	claimStatus, claimFence, claimOwner, claimWorkItemID, found, err := s.readReplayClaimEpoch(ctx, mutation.ClaimID)
	if err != nil {
		return err
	}
	if !found {
		return ErrWorkflowClaimRejected
	}
	if claimStatus != string(wantClaimStatus) ||
		!claimFence.Valid ||
		claimFence.Int64 != mutation.FencingToken ||
		claimOwner != mutation.OwnerID ||
		claimWorkItemID != mutation.WorkItemID {
		return ErrWorkflowClaimRejected
	}
	itemStatus, itemFence, found, err := s.readReplayItemEpoch(ctx, mutation.WorkItemID)
	if err != nil {
		return err
	}
	if !found {
		return ErrWorkflowClaimRejected
	}
	if itemStatus != string(wantItemStatus) ||
		!itemFence.Valid ||
		itemFence.Int64 != mutation.FencingToken {
		return ErrWorkflowClaimRejected
	}
	return nil
}

// readReplayClaimEpoch reads one claim epoch by primary key: status, fencing,
// owner, and the bound work item id. A read or scan failure surfaces loudly;
// only a missing epoch is a rejection, never a masked store error.
func (s *WorkflowControlStore) readReplayClaimEpoch(ctx context.Context, claimID string) (string, sql.NullInt64, string, string, bool, error) {
	rows, err := s.database.QueryContext(ctx, replayAckClaimQuery, claimID)
	if err != nil {
		return "", sql.NullInt64{}, "", "", false, fmt.Errorf("read replayed workflow claim: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", sql.NullInt64{}, "", "", false, fmt.Errorf("read replayed workflow claim: %w", err)
		}
		return "", sql.NullInt64{}, "", "", false, nil
	}
	var status, owner, workItemID string
	var fence sql.NullInt64
	if err := rows.Scan(&status, &fence, &owner, &workItemID); err != nil {
		return "", sql.NullInt64{}, "", "", false, fmt.Errorf("scan replayed workflow claim: %w", err)
	}
	if err := rows.Err(); err != nil {
		return "", sql.NullInt64{}, "", "", false, fmt.Errorf("read replayed workflow claim: %w", err)
	}
	return status, fence, owner, workItemID, true, nil
}

// readReplayItemEpoch reads one work item by primary key: status and the
// NULL-safe current fencing token. Like the claim read, failures surface
// loudly and only a missing row rejects.
func (s *WorkflowControlStore) readReplayItemEpoch(ctx context.Context, workItemID string) (string, sql.NullInt64, bool, error) {
	rows, err := s.database.QueryContext(ctx, replayAckItemQuery, workItemID)
	if err != nil {
		return "", sql.NullInt64{}, false, fmt.Errorf("read replayed workflow work item: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", sql.NullInt64{}, false, fmt.Errorf("read replayed workflow work item: %w", err)
		}
		return "", sql.NullInt64{}, false, nil
	}
	var status string
	var fence sql.NullInt64
	if err := rows.Scan(&status, &fence); err != nil {
		return "", sql.NullInt64{}, false, fmt.Errorf("scan replayed workflow work item: %w", err)
	}
	if err := rows.Err(); err != nil {
		return "", sql.NullInt64{}, false, fmt.Errorf("read replayed workflow work item: %w", err)
	}
	return status, fence, true, nil
}
