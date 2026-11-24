// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// markCICDCompletionOwedQuery sets the completion-owed marker on the calling
// ci_cd pass's own item inside the writer's transaction, fenced exactly like
// its acknowledgement (#10972 PR-4). $1 work item id, $2 lease owner, $3 last
// attempt. Zero rows means the lease is gone: the writer rolls back, so a
// zombie pass cannot commit rows or a marker.
const markCICDCompletionOwedQuery = `
UPDATE fact_work_items
SET payload = jsonb_set(payload, '{` + cicdrun.PayloadKeyCompletionOwed + `}', 'true'::jsonb, TRUE)
WHERE work_item_id = $1
  AND stage = 'reducer'
  AND domain = 'ci_cd_run_correlation'
  AND lease_owner = $2
  AND status IN ('claimed', 'running')
  AND claim_until > clock_timestamp()
  AND last_attempt_at = $3
`

// CICDCompletionOwedMarker builds the per-pass completion-owed recorders for
// the ci_cd_run_correlation handler. LeaseOwner is the reducer work queue's
// lease owner, so the marker is fenced like the acknowledgement.
type CICDCompletionOwedMarker struct {
	LeaseOwner string
}

// Recorder returns the recorder for one claimed ci_cd pass.
func (m CICDCompletionOwedMarker) Recorder(intent reducercontract.Intent) cicdrun.CompletionOwedRecorder {
	return cicdCompletionOwedRecorder{workItemID: intent.IntentID, leaseOwner: m.LeaseOwner, claimedAt: claimedAtValue(intent)}
}

type cicdCompletionOwedRecorder struct {
	workItemID string
	leaseOwner string
	claimedAt  time.Time
}

// RecordCompletionOwed marks the item inside the writer's transaction. A lost
// lease fails the write so its rows roll back with it.
func (r cicdCompletionOwedRecorder) RecordCompletionOwed(ctx context.Context, exec factwrite.Execer) error {
	result, err := exec.ExecContext(ctx, markCICDCompletionOwedQuery, r.workItemID, r.leaseOwner, r.claimedAt)
	if err != nil {
		return fmt.Errorf("mark ci/cd completion owed: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark ci/cd completion owed rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("mark ci/cd completion owed: lease on %q is no longer held", r.workItemID)
	}
	return nil
}
