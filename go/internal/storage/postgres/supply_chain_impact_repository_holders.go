// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// findRepositoryFindingHoldersQuery returns the scopes that hold an active
// finding row for any of the given repository ids, other than the asking
// scope. It reads the writer's own output through
// fact_records_supply_chain_impact_repository_lookup_idx (leading expression
// payload->>'repository_id', partial on the fact kind and is_tombstone =
// FALSE), so the fact kind is a literal here: a bound parameter would not
// prove the partial-index predicate under a generic plan. Joining the scope's
// active generation drops rows left behind by superseded generations. The
// #9874 marker calls it when its package-keyed diff may have missed a removal
// (an unreadable prior view, a walk over the depth cap, or a skipped pruned
// intermediate), so a stale finding for a package the repository no longer
// declares is re-derived (#10059).
//
// Performance Evidence: docs/internal/evidence/10059-repository-reopen.md
// (PG 18, synthetic finding census at 1x, 5x, and 10x; under the 10 s
// holder-read statement timeout).
const findRepositoryFindingHoldersQuery = `
SELECT DISTINCT finding.scope_id
FROM fact_records AS finding
JOIN ingestion_scopes AS scope
  ON scope.scope_id = finding.scope_id
 AND scope.active_generation_id = finding.generation_id
WHERE finding.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
  AND finding.is_tombstone = FALSE
  AND finding.payload->>'repository_id' = ANY($1::text[])
  AND finding.scope_id <> $2
ORDER BY finding.scope_id
`

// FindRepositoryFindingHolders returns the scopes other than ownScopeID that
// hold an active finding row for any of repositoryIDs, sorted. It runs as one
// read-only transaction under the holder-read statement timeout and fails with
// supplychaincore.ErrFanOutReadTimeout when the timeout fires. An empty
// repository list returns nothing without touching the database.
func (s SupplyChainImpactFanOutStore) FindRepositoryFindingHolders(
	ctx context.Context,
	ownScopeID string,
	repositoryIDs []string,
) ([]string, error) {
	if len(repositoryIDs) == 0 {
		return nil, nil
	}
	return s.readScopeIDs(ctx, findRepositoryFindingHoldersQuery, repositoryIDs, ownScopeID)
}
