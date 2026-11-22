// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The production store provides the #11147 dead-letter readings.
var _ telemetry.SupplyChainImpactDeadLetterObserver = (*QueueObserverStore)(nil)

// supplyChainImpactDeadLetterUnclassified labels a dead letter with no
// failure class, so the gauge never carries an empty label value.
const supplyChainImpactDeadLetterUnclassified = "unclassified"

// supplyChainImpactActiveDeadLettersQuery counts the supply_chain_impact
// reducer items that are dead letters at their scope's active generation, by
// failure class, and the subset that still owes a holder fan-out (#11147). A
// dead letter at a superseded generation pins nothing (the winners read only
// active generations), so the join to the scope's active generation is the
// definition, not a filter for speed.
const supplyChainImpactActiveDeadLettersQuery = `
SELECT COALESCE(work.failure_class, '') AS failure_class,
       count(*) AS total,
       count(*) FILTER (
           WHERE work.payload ? '` + supplychaincore.PayloadKeyFanOutOwed + `'
              OR work.payload ? '` + supplychaincore.PayloadKeyFanOutRemaining + `'
       ) AS owing_fanout
FROM fact_work_items AS work
JOIN ingestion_scopes AS scope
  ON scope.scope_id = work.scope_id
 AND scope.active_generation_id = work.generation_id
WHERE work.stage = 'reducer'
  AND work.domain = 'supply_chain_impact'
  AND work.status = 'dead_letter'
GROUP BY 1
ORDER BY 1
`

// SupplyChainImpactActiveDeadLetters returns the active-generation
// supply_chain_impact dead letters by failure class (#11147). It implements
// telemetry.SupplyChainImpactDeadLetterObserver.
func (s *QueueObserverStore) SupplyChainImpactActiveDeadLetters(ctx context.Context) ([]telemetry.SupplyChainImpactDeadLetterCount, error) {
	if s.queryer == nil {
		return nil, fmt.Errorf("queue observer queryer is required")
	}
	rows, err := s.queryer.QueryContext(ctx, supplyChainImpactActiveDeadLettersQuery)
	if err != nil {
		return nil, fmt.Errorf("supply chain impact active dead letters: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var counts []telemetry.SupplyChainImpactDeadLetterCount
	for rows.Next() {
		var count telemetry.SupplyChainImpactDeadLetterCount
		if err := rows.Scan(&count.FailureClass, &count.Total, &count.OwingFanOut); err != nil {
			return nil, fmt.Errorf("supply chain impact active dead letters scan: %w", err)
		}
		if count.FailureClass == "" {
			count.FailureClass = supplyChainImpactDeadLetterUnclassified
		}
		counts = append(counts, count)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("supply chain impact active dead letters: %w", err)
	}
	return counts, nil
}
