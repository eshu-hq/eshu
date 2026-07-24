// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/lib/pq"
)

// producerScopeQuiescenceSQL returns the scopes of a set of collector kinds that
// are active and have NO live projector work item still running. Its NOT EXISTS
// body is byte-equivalent to the production reducer claim query's projector-drain
// fence (reducer_queue_claim_query.go): it rides fact_work_items_scope_generation_idx
// (scope_id-anchored) rather than scanning the work-items table. See
// docs/internal/evidence/5709-quiescence-probe.md for the EXPLAIN proof
// (index-backed, 0.55 ms on the 500-scope x 50k-work-item worst case).
const producerScopeQuiescenceSQL = `SELECT s.scope_id
FROM ingestion_scopes AS s
WHERE s.collector_kind = ANY($1)
  AND s.active_generation_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM fact_work_items AS projector_work
      WHERE projector_work.stage = 'projector'
        AND projector_work.scope_id = s.scope_id
        AND projector_work.status IN ('pending', 'retrying', 'claimed', 'running')
  )`

// ProducerScopeQuiescence reports which scopes of the given collector kinds are
// quiescent-active: their generation is active and no projector work item for the
// scope is still pending/retrying/claimed/running. A cross-scope consumer whose
// declared producer scope is NOT in the returned set must defer (return the
// non-counting crossScopeProducerNotReadyError) rather than write an empty-join
// decision that never re-runs (#5709).
//
// The returned map is keyed by scope_id for O(1) membership. An empty
// collectorKinds set queries nothing and returns an empty map.
func ProducerScopeQuiescence(
	ctx context.Context,
	db Queryer,
	collectorKinds []string,
) (map[string]struct{}, error) {
	quiescent := make(map[string]struct{})
	if len(collectorKinds) == 0 {
		return quiescent, nil
	}
	if db == nil {
		return nil, fmt.Errorf("producer scope quiescence: querier is required")
	}

	rows, err := db.QueryContext(ctx, producerScopeQuiescenceSQL, pq.StringArray(collectorKinds))
	if err != nil {
		return nil, fmt.Errorf("query producer scope quiescence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, fmt.Errorf("scan producer scope quiescence row: %w", err)
		}
		quiescent[scopeID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate producer scope quiescence rows: %w", err)
	}

	return quiescent, nil
}
