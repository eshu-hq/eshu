// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// defaultCatchUpListLimit is the fallback bound
// ListActiveStateSnapshotScopes applies when a caller passes a non-positive
// limit, mirroring projector.ConfigStateDriftCatchUpSweeper's own default
// (issue #5593 P1-1) so a misconfigured caller still gets a bounded scan
// instead of an unbounded one or a Postgres error from `LIMIT 0`/negative.
const defaultCatchUpListLimit = 500

// listActiveStateSnapshotScopesLimitedQuery is listActiveStateSnapshotScopesQuery
// (drift_enqueue.go) with a LIMIT clause. The two queries MUST scan the same
// active state_snapshot:* set -- bootstrap Phase 3.5 and the reducer's
// catch-up sweep both need to see identical scope truth -- but the catch-up
// sweep runs on a recurring interval from the reducer and must not let a
// large corpus turn every tick into an unbounded scan the way the one-shot
// bootstrap sweep safely can.
const listActiveStateSnapshotScopesLimitedQuery = `
SELECT scope.scope_id, scope.active_generation_id
FROM ingestion_scopes AS scope
WHERE scope.scope_id LIKE 'state_snapshot:%'
  AND scope.active_generation_id IS NOT NULL
ORDER BY scope.scope_id ASC
LIMIT $1
`

// ListActiveStateSnapshotScopes implements
// projector.ActiveStateSnapshotScopeLister for
// projector.ConfigStateDriftCatchUpSweeper (issue #5593 P1-1): it lists up to
// `limit` active state_snapshot:* scopes, the same active set
// listActiveStateSnapshotScopes (bootstrap Phase 3.5) scans, so the reducer's
// periodic catch-up sweep converges any generation either of the other two
// config_state_drift producers (bootstrap Phase 3.5, the ingester's runtime
// delta-trigger) missed within its own bounded interval.
func (s IngestionStore) ListActiveStateSnapshotScopes(ctx context.Context, limit int) ([]projector.PendingConfigStateDriftScope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("ingestion store db is required")
	}
	if limit <= 0 {
		limit = defaultCatchUpListLimit
	}

	rows, err := s.db.QueryContext(ctx, listActiveStateSnapshotScopesLimitedQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("list active state_snapshot scopes for catch-up sweep: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scopes []projector.PendingConfigStateDriftScope
	for rows.Next() {
		var scopeID string
		var generationID string
		if err := rows.Scan(&scopeID, &generationID); err != nil {
			return nil, fmt.Errorf("scan active state_snapshot scope for catch-up sweep: %w", err)
		}
		scopeID = strings.TrimSpace(scopeID)
		generationID = strings.TrimSpace(generationID)
		if scopeID == "" || generationID == "" {
			continue
		}
		scopes = append(scopes, projector.PendingConfigStateDriftScope{
			ScopeID:      scopeID,
			GenerationID: generationID,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active state_snapshot scopes for catch-up sweep: %w", err)
	}
	return scopes, nil
}
