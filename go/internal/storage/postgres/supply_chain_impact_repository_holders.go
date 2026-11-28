// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// findRepositoryFindingHoldersQuery returns the scopes that hold an active
// finding support for any of the given repository ids, other than the asking
// scope. It reads the per-support table through
// supply_chain_impact_finding_supports_repository_idx (#11197): the table holds
// live finding facts only, so no tombstone predicate applies, and the empty
// repository id the table stores for a finding without one never matches, so a
// blank lookup id cannot match findings that name no repository (the fact read
// compared the payload key, which a missing key never equalled). Joining the
// scope's active generation drops rows left behind by superseded generations.
// The #9874 marker calls it when its package-keyed diff may have missed a
// removal (an unreadable prior view, a walk over the depth cap, or a skipped
// pruned intermediate), so a stale finding for a package the repository no
// longer declares is re-derived (#10059).
//
// Performance Evidence: (#10059)
// (PG 18, synthetic finding census at 1x, 5x, and 10x; under the 10 s
// holder-read statement timeout). #11197: on 300,000 supports the read is
// 118 buffers and 0.59 ms against the fact read's 112 and 0.64 ms, through the
// existing repository index (#11197 (retired evidence record)).
const findRepositoryFindingHoldersQuery = `
SELECT DISTINCT support.scope_id
FROM supply_chain_impact_finding_supports AS support
JOIN ingestion_scopes AS scope
  ON scope.scope_id = support.scope_id
 AND scope.active_generation_id = support.generation_id
WHERE support.repository_id = ANY($1::text[])
  AND support.repository_id <> ''
  AND support.scope_id <> $2
ORDER BY support.scope_id
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
	return s.readScopeIDsFromSupports(ctx, telemetry.SupplyChainImpactSupportsReaderRepositoryHolders, findRepositoryFindingHoldersQuery, repositoryIDs, ownScopeID)
}
