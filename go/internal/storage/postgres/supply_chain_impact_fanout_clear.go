// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// clearSupplyChainImpactReplayFlagQuery clears one item's
// cross_scope_replay_required only while its fan-out stamp still equals the
// one the pass saw at claim time (#9740). A newer stamp turns the clear into
// a no-op (zero rows) so the pass reruns once under the newer change. The
// lease fence matches the acknowledgement's: only the worker holding the
// claim may clear the flag.
const clearSupplyChainImpactReplayFlagQuery = `
UPDATE fact_work_items
SET cross_scope_replay_required = FALSE,
    updated_at = $1
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND lease_owner = $3
  AND status IN ('claimed', 'running')
  AND claim_until > clock_timestamp()
  AND last_attempt_at = $4
  AND payload->'` + supplychaincore.PayloadKeyFanOutOrigin + `' = $5::jsonb
`

// clearSupplyChainImpactReplayLeaseQuery re-checks the lease fence when the
// conditional clear matched no row, so a lost lease stays an error (the pass
// must fail and another worker redo it) while a newer stamp is a clean
// no-op (the pass reruns under that stamp).
const clearSupplyChainImpactReplayLeaseQuery = `
SELECT 1
FROM fact_work_items
WHERE work_item_id = $1
  AND stage = 'reducer'
  AND lease_owner = $2
  AND status IN ('claimed', 'running')
  AND claim_until > clock_timestamp()
  AND last_attempt_at = $3
`

// ClearReplayFlagIfStampSeen clears the calling pass's replay flag while its
// fan-out stamp still equals seen, fenced on its lease like the
// acknowledgement. It reports whether the flag was cleared: false means a
// newer stamp landed since the claim and the pass must rerun.
func (s SupplyChainImpactFanOutStore) ClearReplayFlagIfStampSeen(
	ctx context.Context,
	intent reducercontract.Intent,
	seen map[string]any,
) (bool, error) {
	if s.DB == nil {
		return false, errors.New("supply chain impact fan-out database is required")
	}
	expected, err := json.Marshal(seen)
	if err != nil {
		return false, fmt.Errorf("marshal supply chain impact fan-out stamp: %w", err)
	}
	result, err := s.DB.ExecContext(ctx, clearSupplyChainImpactReplayFlagQuery,
		s.now(), intent.IntentID, s.LeaseOwner, claimedAtValue(intent), string(expected))
	if err != nil {
		return false, fmt.Errorf("clear supply chain impact replay flag: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("clear supply chain impact replay flag rows affected: %w", err)
	}
	if affected > 0 {
		return true, nil
	}
	held, err := s.DB.QueryContext(ctx, clearSupplyChainImpactReplayLeaseQuery,
		intent.IntentID, s.LeaseOwner, claimedAtValue(intent))
	if err != nil {
		return false, fmt.Errorf("recheck supply chain impact replay lease: %w", err)
	}
	leaseHeld := held.Next()
	closeErr := held.Close()
	if err := held.Err(); err != nil {
		return false, fmt.Errorf("iterate supply chain impact replay lease: %w", err)
	}
	if closeErr != nil {
		return false, fmt.Errorf("close supply chain impact replay lease: %w", closeErr)
	}
	if !leaseHeld {
		return false, fmt.Errorf("clear supply chain impact replay flag: lease on %q is no longer held", intent.IntentID)
	}
	return false, nil
}
