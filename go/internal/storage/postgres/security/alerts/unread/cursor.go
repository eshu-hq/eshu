// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unread

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// FanoutCursorQuery returns the fan-out position of one scope: the repository
// the next org fan-out claim starts at and, while a lap is in progress, the
// last-position repository whose failed read closed it (#8946). It reads the
// newest pending-or-active generation with the ordering
// activeGenerationFreshnessQuery uses to compare freshness hints, not
// ingestion_scopes.active_generation_id: a claim that lands between a commit and
// its activation must read the cursor that commit carried, or it would re-read
// the same repositories, match that generation's hint, and be skipped with no
// progress. The read-state lookup is a lateral primary-key probe, so it never
// scans the read-state table; both columns are NULL for a generation committed
// before the cursor existed, which starts at the top. $1 scope id.
// It is exported so the live plan proof can EXPLAIN the exact statement.
const FanoutCursorQuery = `
SELECT state.fanout_next_repository, state.fanout_lap_repository
FROM (
    SELECT generation.generation_id
    FROM scope_generations AS generation
    WHERE generation.scope_id = $1
      AND generation.status IN ('pending', 'active')
      AND COALESCE(generation.freshness_hint, '') <> ''
    ORDER BY generation.ingested_at DESC, generation.generation_id DESC
    LIMIT 1
) AS newest
LEFT JOIN LATERAL (
    SELECT read_state.fanout_next_repository, read_state.fanout_lap_repository
    FROM security_alert_target_read_states AS read_state
    WHERE read_state.generation_id = newest.generation_id
) AS state ON TRUE`

// CursorReader reads the org fan-out cursor of a scope from the generation
// read state the Writer commits.
type CursorReader struct {
	queryer db.Queryer
}

// NewCursorReader returns a CursorReader over queryer.
func NewCursorReader(queryer db.Queryer) *CursorReader {
	return &CursorReader{queryer: queryer}
}

// FanoutCursor returns the committed fan-out position of scopeID: the
// allowlisted repository the next claim starts at and the lap repository, both
// "" for the top and no lap (no generation, a generation with no read state, or
// one that carried no cursor). A query failure is returned, never answered with
// the top: starting at the top on an error would silently restore the bug the
// cursor fixes.
func (r *CursorReader) FanoutCursor(ctx context.Context, scopeID string) (securityalerts.FanoutCursor, error) {
	rows, err := r.queryer.QueryContext(ctx, FanoutCursorQuery, scopeID)
	if err != nil {
		return securityalerts.FanoutCursor{}, fmt.Errorf("read security alert fan-out cursor: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var cursor securityalerts.FanoutCursor
	if rows.Next() {
		var next, lap sql.NullString
		if err := rows.Scan(&next, &lap); err != nil {
			return securityalerts.FanoutCursor{}, fmt.Errorf("scan security alert fan-out cursor: %w", err)
		}
		cursor = securityalerts.FanoutCursor{NextRepository: next.String, LapRepository: lap.String}
	}
	if err := rows.Err(); err != nil {
		return securityalerts.FanoutCursor{}, fmt.Errorf("read security alert fan-out cursor rows: %w", err)
	}
	return cursor, nil
}
