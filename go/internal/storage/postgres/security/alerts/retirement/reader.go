// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// CandidatesQuery returns the scopes of the listed org targets that still need
// a retirement: the base scope itself ($1 by scope id) or a shard scope whose
// parent is a base ($1 by parent_scope_id), with an active generation, not in
// the planned set ($2), and whose active generation is not already a
// retirement. A retirement generation commits a read state with
// repositories_total = 0; a generation with no read state (committed before
// #8943) is still a candidate. The two scope predicates combine as a BitmapOr
// of ingestion_scopes_parent_idx and the primary key, and the read-state join
// is a primary-key probe on generation_id, so the read never scans either
// table. $3 bounds the rows. It is exported so the live plan proof can
// EXPLAIN the exact statement.
const CandidatesQuery = `
SELECT scope.scope_id
FROM ingestion_scopes AS scope
LEFT JOIN security_alert_target_read_states AS state
  ON state.generation_id = scope.active_generation_id
WHERE (scope.parent_scope_id = ANY($1::text[]) OR scope.scope_id = ANY($1::text[]))
  AND scope.scope_id <> ALL($2::text[])
  AND scope.collector_kind = 'security_alert'
  AND scope.active_generation_id IS NOT NULL
  AND (state.generation_id IS NULL OR state.repositories_total <> 0)
ORDER BY scope.scope_id
LIMIT $3`

// Reader reads retirement candidates for the coordinator's security-alert
// planner. It satisfies the planner's ShardScopeReader port.
type Reader struct {
	queryer db.Queryer
}

// NewReader returns a Reader over queryer.
func NewReader(queryer db.Queryer) *Reader {
	return &Reader{queryer: queryer}
}

// RetirementCandidates returns at most limit scope ids of the org targets
// named by baseScopeIDs that are not in plannedScopeIDs and still hold a
// non-retired active generation, ordered by scope id. No base issues no
// query. A query or row error is returned, never answered with an empty
// list: an empty list means nothing needs retiring.
func (r *Reader) RetirementCandidates(
	ctx context.Context,
	baseScopeIDs []string,
	plannedScopeIDs []string,
	limit int,
) ([]string, error) {
	if len(baseScopeIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		return nil, fmt.Errorf("security alert retirement candidate limit must be positive, got %d", limit)
	}
	if plannedScopeIDs == nil {
		plannedScopeIDs = []string{}
	}
	rows, err := r.queryer.QueryContext(ctx, CandidatesQuery, array.Of(baseScopeIDs), array.Of(plannedScopeIDs), limit)
	if err != nil {
		return nil, fmt.Errorf("read security alert retirement candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]string, 0, limit)
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, fmt.Errorf("scan security alert retirement candidate: %w", err)
		}
		out = append(out, scopeID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read security alert retirement candidate rows: %w", err)
	}
	return out, nil
}
