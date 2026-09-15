// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// ActivateResolutionGenerationForClaim activates a relationship generation
// only while the exact reducer claim that produced it remains live. Locking the
// queue row in the same statement serializes publication against recovery's
// deletion; matching last_attempt_at rejects an older execution after reclaim.
func (s *RelationshipStore) ActivateResolutionGenerationForClaim(
	ctx context.Context,
	generationID string,
	scopeID string,
	workItemID string,
	claimedAt time.Time,
) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		activateResolutionGenerationForClaimSQL,
		generationID,
		scopeID,
		now,
		now,
		workItemID,
		claimedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("activate resolution generation for reducer claim: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("activate resolution generation for reducer claim: rows affected: %w", err)
	}
	if affected == 0 {
		return reducercontract.ErrExecutionClaimRejected
	}
	return nil
}
