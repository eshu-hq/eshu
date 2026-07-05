// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// SharedIntentStore answers the first-projection probe the shared projection
// runner uses to skip a scope's whole-scope edge retract when there is nothing
// to retract (#3624).
var _ reducer.FirstProjectionLookup = (*SharedIntentStore)(nil)

// scopeHasPriorGenerationSQL reports whether the scope has ANY generation other
// than the current one, in any status. For the shared-projection domains edges
// are written on acceptance (before a generation activates), so a generation
// that was accepted and then superseded while still pending can have written
// edges without ever setting activated_at. Keying the first-projection skip on
// activation would miss those edges; keying it on "no other generation exists at
// all" is correct — the only scope with zero prior edges of any generation is
// one whose sole generation is the current one (a true first projection). On a
// cold first ingest a scope has exactly one generation, so the skip still fires.
// The scope_generations_scope_idx (scope_id, ...) index serves the scope_id
// prefix; generation_id is applied as a cheap post-scan filter bounded by the
// (small) number of generations per scope.
const scopeHasPriorGenerationSQL = `
SELECT EXISTS (
    SELECT 1
    FROM scope_generations
    WHERE scope_id = $1
      AND generation_id <> $2
)`

// ScopeHasPriorGeneration reports whether the scope has any generation other
// than currentGenerationID. When it returns false the scope's only generation
// is the current one — a true first projection — so its whole-scope edge retract
// is a guaranteed no-op and the shared projection runner skips it (#3624).
// Returning true (or an error) leaves the retract running, so every re-ingest,
// and any scope that has ever had another generation project (activated or not),
// still retracts.
func (s *SharedIntentStore) ScopeHasPriorGeneration(
	ctx context.Context,
	scopeID string,
	currentGenerationID string,
) (bool, error) {
	sqlRows, err := s.db.QueryContext(
		ctx,
		scopeHasPriorGenerationSQL,
		scopeID,
		currentGenerationID,
	)
	if err != nil {
		return false, fmt.Errorf("query prior generation for scope %s: %w", scopeID, err)
	}
	defer func() { _ = sqlRows.Close() }()

	if !sqlRows.Next() {
		return false, nil
	}
	var exists bool
	if err := sqlRows.Scan(&exists); err != nil {
		return false, fmt.Errorf("scan prior generation for scope %s: %w", scopeID, err)
	}
	return exists, sqlRows.Err()
}
