// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// findInFlightSupplyChainImpactScopesQuery returns the scopes, other than the
// asking one, whose supply_chain_impact item at the scope's ACTIVE generation is
// claimed or running. A pass in flight may have loaded its evidence before the
// asking pass's source facts were visible, and a scope whose newly active
// generation has no finding rows yet is invisible to the row read. An item at an
// older generation is harmless (its pass writes a superseded generation) and is
// excluded by the join; a pending or retrying item has not loaded evidence yet.
// The set is bounded by the number of passes in flight at once and uses
// fact_work_items_stage_domain_status_idx (stage, domain, status). Argument: $1
// the asking scope id.
const findInFlightSupplyChainImpactScopesQuery = `
SELECT DISTINCT work.scope_id
FROM fact_work_items AS work
JOIN ingestion_scopes AS scope
  ON scope.scope_id = work.scope_id
 AND scope.active_generation_id = work.generation_id
WHERE work.stage = 'reducer'
  AND work.domain = 'supply_chain_impact'
  AND work.status IN ('claimed', 'running')
  AND work.scope_id <> $1
ORDER BY work.scope_id
`

// markSupplyChainImpactFanOutOwedQuery sets the owed marker on the calling pass's
// own item, inside the writer's transaction, fenced exactly like an
// acknowledgement. $1 work item id, $2 lease owner, $3 last attempt. Zero rows
// means the lease is gone: the writer rolls back, so a zombie pass cannot commit.
// The marker is the constant true, so the item payload does not grow with the
// number of CVEs changed.
const markSupplyChainImpactFanOutOwedQuery = `
UPDATE fact_work_items
SET payload = jsonb_set(payload, '{` + supplychaincore.PayloadKeyFanOutOwed + `}', 'true'::jsonb, TRUE)
WHERE work_item_id = $1
  AND stage = 'reducer'
  AND lease_owner = $2
  AND status IN ('claimed', 'running')
  AND claim_until > clock_timestamp()
  AND last_attempt_at = $3
`

// FindInFlightScopes returns the scopes other than ownScopeID whose
// supply_chain_impact item at the active generation is claimed or running.
func (s SupplyChainImpactFanOutStore) FindInFlightScopes(ctx context.Context, ownScopeID string) ([]string, error) {
	return s.readScopeIDs(ctx, findInFlightSupplyChainImpactScopesQuery, ownScopeID)
}

// OwedRecorder returns the recorder the writer runs inside its transaction for
// the calling pass's own item.
func (s SupplyChainImpactFanOutStore) OwedRecorder(intent reducercontract.Intent) supplychaincore.FanOutOwedRecorder {
	return supplyChainImpactOwedRecorder{
		workItemID: intent.IntentID,
		leaseOwner: s.LeaseOwner,
		claimedAt:  claimedAtValue(intent),
	}
}

type supplyChainImpactOwedRecorder struct {
	workItemID string
	leaseOwner string
	claimedAt  time.Time
}

// RecordFanOutOwed marks the item inside the writer's transaction. A lost lease
// is a retryable failure: the rows roll back with it and the queue retries.
func (r supplyChainImpactOwedRecorder) RecordFanOutOwed(ctx context.Context, exec factwrite.Execer) error {
	result, err := exec.ExecContext(ctx, markSupplyChainImpactFanOutOwedQuery, r.workItemID, r.leaseOwner, r.claimedAt)
	if err != nil {
		return fmt.Errorf("mark supply chain impact fan-out owed: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark supply chain impact fan-out owed rows affected: %w", err)
	}
	if affected == 0 {
		return supplychaincore.RetryableFanOutError(fmt.Errorf(
			"mark supply chain impact fan-out owed: lease on %q is no longer held", r.workItemID))
	}
	return nil
}
