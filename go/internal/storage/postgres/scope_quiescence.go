// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// producerScopeQuiescenceSQL returns EVERY ingestion scope registered under a
// set of collector kinds, each flagged with whether it is quiescent-active:
// generation active, and no live projector work item.
//
// Reporting the registered scopes and not only the quiescent ones is what lets a
// caller tell two very different situations apart. "This deployment runs no
// collector of this kind at all" is not the same as "a collector of this kind
// exists and has not finished yet", and a caller that sees only an empty
// quiescent set cannot distinguish them.
//
// The NOT EXISTS body asks the same question as the production reducer claim
// query's projector-drain fence (reducer_queue_claim_query.go) on the same
// columns: stage = 'projector', a scope_id correlation, and the same four live
// statuses. The text is not identical, and cannot be -- the claim query
// correlates against fact_work_items.scope_id because it is already scanning
// that table, while this one correlates against s.scope_id from the CTE above.
// The columns the planner has to resolve are the same either way, so both ride
// fact_work_items_scope_generation_idx (scope_id-anchored) rather than scanning
// the work-items table. The EXPLAIN in
// docs/internal/evidence/5709-quiescence-probe.md is what establishes that;
// the resemblance on its own would not.
//
// Keeping the scope filter in a CTE aliased AS s is what leaves the correlation
// a plain column reference the planner can push into the index.
//
// The CTE-plus-LEFT-JOIN shape is deliberate and was measured, because the
// obvious alternative loses the index. Writing the flag as a NOT EXISTS
// expression in the target list lets PostgreSQL 16 hash the subquery instead of
// correlating it, which turns the probe into one sequential scan of
// fact_work_items: 5.16 ms against 0.30 ms for the same seed. See
// docs/internal/evidence/5709-quiescence-probe.md for both plans.
const producerScopeQuiescenceSQL = `WITH registered AS (
    SELECT s.scope_id, s.active_generation_id
    FROM ingestion_scopes AS s
    WHERE s.collector_kind = ANY($1)
), quiescent AS (
    SELECT s.scope_id
    FROM registered AS s
    WHERE s.active_generation_id IS NOT NULL
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS projector_work
          WHERE projector_work.stage = 'projector'
            AND projector_work.scope_id = s.scope_id
            AND projector_work.status IN ('pending', 'retrying', 'claimed', 'running')
      )
)
SELECT registered.scope_id, quiescent.scope_id IS NOT NULL AS quiescent
FROM registered
LEFT JOIN quiescent ON quiescent.scope_id = registered.scope_id`

// ProducerScopeQuiescenceReport answers two questions about one set of producer
// collector kinds, from one query.
//
// Both sets are keyed by scope_id for O(1) membership, and Quiescent is always a
// subset of Registered.
type ProducerScopeQuiescenceReport struct {
	// Registered is every scope of the requested collector kinds, whatever its
	// state. Empty means no such collector runs in this deployment, which a
	// cross-scope consumer must read as "nothing to wait for" rather than as
	// "not ready" -- otherwise a deployment that simply does not run that
	// collector defers its consumers on every claim until they time out.
	Registered map[string]struct{}
	// Quiescent is the subset whose generation is active and whose projector
	// work has drained. A consumer whose producer kind IS registered but has
	// no quiescent scope must defer (return the non-counting
	// crossScopeProducerNotReadyError) rather than write an empty-join
	// decision that never re-runs (#5709).
	Quiescent map[string]struct{}
}

// ProducerScopeQuiescence reports the registered and quiescent-active scopes of
// the given collector kinds in one round trip.
//
// An empty collectorKinds set queries nothing and returns empty sets.
func ProducerScopeQuiescence(
	ctx context.Context,
	db Queryer,
	collectorKinds []string,
) (ProducerScopeQuiescenceReport, error) {
	report := ProducerScopeQuiescenceReport{
		Registered: make(map[string]struct{}),
		Quiescent:  make(map[string]struct{}),
	}
	if len(collectorKinds) == 0 {
		return report, nil
	}
	if db == nil {
		return ProducerScopeQuiescenceReport{}, fmt.Errorf("producer scope quiescence: querier is required")
	}

	rows, err := db.QueryContext(ctx, producerScopeQuiescenceSQL, pgarray.StringArray(collectorKinds))
	if err != nil {
		return ProducerScopeQuiescenceReport{}, fmt.Errorf("query producer scope quiescence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			scopeID   string
			quiescent bool
		)
		if err := rows.Scan(&scopeID, &quiescent); err != nil {
			return ProducerScopeQuiescenceReport{}, fmt.Errorf("scan producer scope quiescence row: %w", err)
		}
		report.Registered[scopeID] = struct{}{}
		if quiescent {
			report.Quiescent[scopeID] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return ProducerScopeQuiescenceReport{}, fmt.Errorf("iterate producer scope quiescence rows: %w", err)
	}

	return report, nil
}
