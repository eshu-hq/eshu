// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var heartbeatDerivedScannerClaimQuery = strings.Replace(
	heartbeatWorkflowClaimQuery, scannerDerivedClaimAuthority,
	scannerDerivedHeartbeatAuthority, 1,
)

// heartbeatDerivedScannerClaim locks the canonical source and authority
// vector before the existing workflow claim. A caller already in a transaction
// keeps those locks through its own commit; a standalone heartbeat owns one
// short transaction for the same ordering.
func (s *WorkflowControlStore) heartbeatDerivedScannerClaim(
	ctx context.Context, mutation workflow.ClaimMutation, leaseTTL time.Duration,
) error {
	if s.beginner != nil {
		tx, err := s.beginner.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin derived scanner heartbeat: %w", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()
		if err := heartbeatDerivedScannerClaimInTx(ctx, tx, mutation, leaseTTL); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit derived scanner heartbeat: %w", err)
		}
		committed = true
		return nil
	}
	tx, ok := s.database.(db.Transaction)
	if !ok {
		return fmt.Errorf("derived scanner heartbeat requires a transaction")
	}
	return heartbeatDerivedScannerClaimInTx(ctx, tx, mutation, leaseTTL)
}

func heartbeatDerivedScannerClaimInTx(ctx context.Context, tx db.Transaction,
	mutation workflow.ClaimMutation, leaseTTL time.Duration,
) error {
	if _, err := lockScannerAttemptAuthority(ctx, tx, mutation.WorkItemID,
		mutation.ClaimID, mutation.FencingToken, mutation.OwnerID); err != nil {
		return err
	}
	return NewWorkflowControlStore(tx).execClaimMutation(ctx, claimOpHeartbeat, mutation,
		heartbeatDerivedScannerClaimQuery,
		mutation.ObservedAt.UTC().Add(leaseTTL))
}
