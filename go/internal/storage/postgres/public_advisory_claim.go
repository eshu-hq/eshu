// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ErrPublicAdvisoryClaimNotCurrent identifies a claim that cannot choose an
// advisory producer route. It must never be treated as a static fallback.
var ErrPublicAdvisoryClaimNotCurrent = errors.New("public advisory claim not current")

const isPublicAdvisoryClaimQuery = `
SELECT EXISTS (
    SELECT 1 FROM scanner_advisory_public_registrations AS registration
    WHERE registration.collector_instance_id=item.collector_instance_id
      AND registration.advisory_scope_id=item.scope_id
      AND registration.active
)
FROM workflow_work_items AS item
JOIN workflow_claims AS claim
  ON claim.claim_id=item.current_claim_id
 AND claim.work_item_id=item.work_item_id
WHERE item.work_item_id=$1
  AND item.collector_kind='vulnerability_intelligence'
  AND item.status='claimed'
  AND item.current_claim_id=$2
  AND item.current_fencing_token=$3
  AND item.current_owner_id=$4
  AND item.lease_expires_at>clock_timestamp()
  AND claim.status='active'
  AND claim.fencing_token=$3
  AND claim.owner_id=$4`

// IsPublicAdvisoryClaim chooses the typed producer route from current database
// registration state for one exact workflow claim. A stale claim returns an
// error, and later stage/publication recheck authority after this read.
func (s *WorkflowControlStore) IsPublicAdvisoryClaim(
	ctx context.Context, mutation workflow.ClaimMutation,
) (bool, error) {
	if s.database == nil {
		return false, fmt.Errorf("workflow control store database is required")
	}
	if mutation.WorkItemID == "" || mutation.ClaimID == "" ||
		mutation.OwnerID == "" || mutation.FencingToken <= 0 {
		return false, ErrPublicAdvisoryClaimNotCurrent
	}
	rows, err := s.database.QueryContext(ctx, isPublicAdvisoryClaimQuery,
		mutation.WorkItemID, mutation.ClaimID,
		mutation.FencingToken, mutation.OwnerID)
	if err != nil {
		return false, fmt.Errorf("lookup public advisory claim: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("lookup public advisory claim: %w", err)
		}
		return false, ErrPublicAdvisoryClaimNotCurrent
	}
	var isPublic bool
	if err := rows.Scan(&isPublic); err != nil {
		return false, fmt.Errorf("scan public advisory claim: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read public advisory claim: %w", err)
	}
	return isPublic, nil
}
