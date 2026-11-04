// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// recordManifestTriggerCompletedQuery stamps the calling marker pass's own
// work item with {at, baseline_generation_id} under
// supplychaincore.PayloadKeyManifestTriggerCompleted (#9874 ruling D1). It is
// fenced exactly like recordSupplyChainImpactFanOutStateQuery and the
// acknowledgement: the lease owner, a live lease, and the attempt it claimed,
// so a pass that lost its lease stamps nothing. It replaces the one key and
// keeps every other payload key, and ReopenHolder touches only the fan-out
// origin key, so a later reopen of the item keeps the stamp. $1 now, $2 work
// item id, $3 lease owner, $4 last attempt, $5 baseline generation id.
const recordManifestTriggerCompletedQuery = `
UPDATE fact_work_items
SET payload = payload || jsonb_build_object('` + supplychaincore.PayloadKeyManifestTriggerCompleted + `',
                  jsonb_build_object('at', $1::timestamptz, 'baseline_generation_id', $5::text)),
    updated_at = $1
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND lease_owner = $3
  AND status IN ('claimed', 'running')
  AND claim_until > clock_timestamp()
  AND last_attempt_at = $4
`

// RecordManifestTriggerCompleted implements the completion stamp of
// supplychaincore.ManifestDependencyGenerationReader. It fails when the lease
// is no longer held, so the pass is retried rather than recorded as complete.
func (s SupplyChainImpactFanOutStore) RecordManifestTriggerCompleted(
	ctx context.Context,
	intent reducercontract.Intent,
	baselineGenerationID string,
) error {
	if s.DB == nil {
		return errors.New("supply chain impact fan-out database is required")
	}
	result, err := s.DB.ExecContext(ctx, recordManifestTriggerCompletedQuery,
		s.now(), intent.IntentID, s.LeaseOwner, claimedAtValue(intent), baselineGenerationID)
	if err != nil {
		return fmt.Errorf("record supply chain impact manifest trigger completion: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("record supply chain impact manifest trigger completion rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("record supply chain impact manifest trigger completion: lease on %q is no longer held", intent.IntentID)
	}
	return nil
}
