// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The production store keeps providing the #10865 reclaim-backlog readings.
var _ telemetry.ReducerClaimedExpiredObserver = (*QueueObserverStore)(nil)

// reducerClaimedExpiredQuery counts reducer work items sitting on an expired
// claim lease and reports the oldest such lease's age in seconds (#10865).
// The predicate mirrors the claim's reclaim_rank lease test (status claimed
// or running with claim_until at or before the read clock), so the gauge
// counts the expired-lease rows the claim now floats first — a nonzero
// count names the
// reclaim backlog, and the age names how long its head has waited. A claimed
// row with a NULL claim_until (no lease stamped, anomalous: the claim UPDATE
// always stamps one) is excluded here but floats first in the batch by
// status rank; such a row needs an owner, not a lease wait, so the metric
// stays silent about it by design.
//
// Claimed/running rows are never fenced as stale generations (the active
// generation filter only applies to pending/retrying), so no generation CTE
// is needed here; every counted row is claimable.
const reducerClaimedExpiredQuery = `
SELECT COUNT(*) AS count,
    COALESCE(MAX(EXTRACT(EPOCH FROM ($1 - claim_until))), 0) AS oldest_age_seconds
FROM fact_work_items
WHERE stage = 'reducer'
  AND status IN ('claimed', 'running')
  AND claim_until <= $1
`

// ReducerClaimedExpiredCounts returns how many reducer work items hold an
// expired claimed or running lease and the age in seconds of the oldest such
// lease (#10865). Zero/zero means no reclaim backlog is outstanding. It
// implements telemetry.ReducerClaimedExpiredObserver.
func (s *QueueObserverStore) ReducerClaimedExpiredCounts(ctx context.Context) (int64, float64, error) {
	if s.queryer == nil {
		return 0, 0, fmt.Errorf("queue observer queryer is required")
	}

	rows, err := s.queryer.QueryContext(ctx, reducerClaimedExpiredQuery, s.now())
	if err != nil {
		return 0, 0, fmt.Errorf("reducer claimed-expired counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var count int64
	var age float64
	if rows.Next() {
		if err := rows.Scan(&count, &age); err != nil {
			return 0, 0, fmt.Errorf("reducer claimed-expired counts scan: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("reducer claimed-expired counts: %w", err)
	}

	return count, age, nil
}
