// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// hasOutOfStampActiveSupplyChainImpactRowsQuery is the #10415 fallback
// trigger: one bounded existence probe for active finding rows of one
// generation whose CVE id the stamp does not name. The fact kind is a literal
// so the planner proves the predicate of the partial finding indexes under a
// generic plan, the same shape the seed read and the writer's stored read
// use. LIMIT 1 bounds the probe to one row: a quiet holder with no drift
// pays one index probe on its scoped path, and a full pass never runs it.
// Rows without a CVE id never match an equality on the stamp, so the probe
// names them explicitly empty-excluded: they are not CVE drift and the next
// genuine full pass converges them, while counting them would turn the
// fallback into every reopen for holders carrying them. No new index: the
// predicate reuses the partial finding-index shape the seed read proves.
const hasOutOfStampActiveSupplyChainImpactRowsQuery = `
SELECT 1
FROM fact_records
WHERE fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
  AND scope_id = $1
  AND generation_id = $2
  AND is_tombstone = FALSE
  AND payload->>'cve_id' <> ''
  AND NOT (payload->>'cve_id' = ANY($3::text[]))
LIMIT 1
`

// HasOutOfStampActiveSupplyChainImpactRows reports whether one (scope,
// generation) holds an active finding row outside a stamped CVE set. An empty
// stamp reads nothing: the caller runs full without it. A tombstoned copy
// never triggers: a retracted row is already reaped.
func (s FactStore) HasOutOfStampActiveSupplyChainImpactRows(
	ctx context.Context,
	scopeID string,
	generationID string,
	cveIDs []string,
) (bool, error) {
	if s.database == nil {
		return false, fmt.Errorf("fact store database is required")
	}
	if len(cveIDs) == 0 {
		return false, nil
	}
	var has bool
	err := withReadOnlyRepeatableRead(ctx, s.database, func(queryer db.Queryer) error {
		rows, err := queryer.QueryContext(ctx, hasOutOfStampActiveSupplyChainImpactRowsQuery, scopeID, generationID, cveIDs)
		if err != nil {
			return fmt.Errorf("read out-of-stamp supply chain impact finding rows: %w", err)
		}
		defer func() { _ = rows.Close() }()
		if rows.Next() {
			has = true
		}
		return rows.Err()
	})
	if err != nil {
		return false, err
	}
	return has, nil
}
