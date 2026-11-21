// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The production store keeps providing the #11081 orphan readings.
var _ telemetry.ReducerLeaseOrphanObserver = (*QueueObserverStore)(nil)

// reducerLeaseOrphanQuery counts reducer lease-mirror rows no live main row
// owns (#11081). The predicate is the claim-time liveness definition shared
// with the claim CTEs, the heartbeat, and migration 326: a lease row is
// live iff its main row matches work item, stage, claimed/running status,
// unexpired claim, owner, and last attempt. Missing-main rows count too
// (the FK cascade normally prevents them; counting them keeps the gauge
// honest if the cascade is ever bypassed). NOW is the read clock, matching
// the other main-table probes on this store.
const reducerLeaseOrphanQuery = `
SELECT COUNT(*)
FROM fact_work_item_leases AS lease
WHERE lease.stage = 'reducer'
  AND NOT EXISTS (
      SELECT 1 FROM fact_work_items AS holder
      WHERE holder.work_item_id = lease.work_item_id
        AND holder.stage = 'reducer'
        AND holder.status IN ('claimed', 'running')
        AND holder.claim_until > $1
        AND holder.lease_owner = lease.lease_owner
        AND holder.last_attempt_at = lease.last_attempt_at
  )
`

// ReducerLeaseOrphanRows returns how many reducer lease-mirror rows have no
// matching live main row (#11081). It implements
// telemetry.ReducerLeaseOrphanObserver; a sustained nonzero value means a
// hold-end leaked its lease clear or version-skewed writers are still
// active, while brief blips from expired-awaiting-reclaim are expected.
func (s *QueueObserverStore) ReducerLeaseOrphanRows(ctx context.Context) (int64, error) {
	return s.countProjectorClaimInvariant(ctx, reducerLeaseOrphanQuery, "reducer lease orphan rows", s.now())
}
