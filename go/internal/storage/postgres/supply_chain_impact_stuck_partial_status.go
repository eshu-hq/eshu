// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// supplyChainImpactStuckPartialStatusQuery serves the #10389 degraded reason:
// one row per scope whose last recorded supply-chain impact pass ended
// partial. The table holds one row per scope, so the scan is over stuck
// scopes only, ordered for a stable status payload. No secondary index:
// partial_evidence alone is low-cardinality and the table is scope-sized.
const supplyChainImpactStuckPartialStatusQuery = `
SELECT scope_id, partial_causes, partial_since
FROM supply_chain_impact_scope_pass_state
WHERE partial_evidence
ORDER BY scope_id ASC
`

func readSupplyChainStuckPartialScopes(
	ctx context.Context,
	queryer db.Queryer,
) ([]statuspkg.SupplyChainStuckPartialScope, error) {
	rows, err := queryer.QueryContext(ctx, supplyChainImpactStuckPartialStatusQuery)
	if err != nil {
		return nil, fmt.Errorf("read supply chain impact stuck partial scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	scopes := []statuspkg.SupplyChainStuckPartialScope{}
	for rows.Next() {
		var scope statuspkg.SupplyChainStuckPartialScope
		var causes array.StringArray
		if err := rows.Scan(&scope.ScopeID, &causes, &scope.PartialSince); err != nil {
			return nil, fmt.Errorf("read supply chain impact stuck partial scopes: %w", err)
		}
		scope.Causes = []string(causes)
		scope.PartialSince = scope.PartialSince.UTC()
		if scope.ScopeID == "" {
			continue
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read supply chain impact stuck partial scopes: %w", err)
	}
	return scopes, nil
}
