// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// defaultCatchUpListLimit is the fallback bound
// ListActiveStateSnapshotScopes applies when a caller passes a non-positive
// limit, mirroring projector.ConfigStateDriftCatchUpSweeper's own default
// (issue #5593) so a misconfigured caller still gets a bounded scan instead
// of an unbounded one or a Postgres error from `LIMIT 0`/negative.
const defaultCatchUpListLimit = 500

// listActiveStateSnapshotScopesLimitedQuery lists active state_snapshot
// scopes for the reducer's recurring catch-up sweep, bounded by $1.
//
// Filters on scope_kind (an indexed equality), NOT
// `scope_id LIKE 'state_snapshot:%'` the way listActiveStateSnapshotScopesQuery
// (drift_enqueue.go, the one-shot bootstrap Phase 3.5 sweep) does. The two
// queries still scan the SAME active set -- NewTerraformStateSnapshotScope
// (go/internal/scope/tfstate.go) sets ScopeID to the "state_snapshot:..."
// prefix and ScopeKind to scope.KindStateSnapshot in the same struct
// literal, so every row either predicate matches also matches the other,
// for every scope this codebase writes today -- but this query runs
// forever on a fixed interval from the steady-state reducer, not once from
// a one-shot bootstrap pass, so its predicate must be servable by an
// equality-indexed partial index rather than relying on the LIKE-prefix
// fallback plan bootstrap's unbounded, LIMIT-free version safely gets away
// with. Measured: the LIKE-based shape forced a near-full ingestion_scopes_pkey
// scan (~76 ms at 500K rows, ~506 ms at 2M rows, growing with total corpus
// size); the scope_kind equality shape, backed by
// ingestion_scopes_active_state_snapshot_idx (migration 091), stays flat at
// ~0.6-1.2 ms regardless of total corpus size. See
// docs/internal/evidence/5593-config-state-drift-catchup-lister-query.md for
// the full EXPLAIN (ANALYZE, BUFFERS) ladder.
//
// scope_kind is INLINED as a SQL literal here, NOT bound as a query
// parameter -- a review finding (issue #5593): this lister only ever targets
// one scope_kind (there is exactly one production caller,
// projector.ConfigStateDriftCatchUpSweeper.RunOnce, and it never varies the
// value), so there is no reason to bind it. Postgres cannot statically prove
// a bound `scope_kind = $1` satisfies the partial index's
// `WHERE scope_kind = 'state_snapshot'` predicate once a forced generic plan
// is in play -- measured: a forced generic plan fell back to a full
// ingestion_scopes_pkey scan, ~296 ms at 2M rows, silently reproducing the
// exact regression this migration exists to fix. Inlining the literal
// removes that failure mode instead of documenting and hoping the planner's
// custom-plan heuristic never changes (data skew, a Postgres version change,
// or a connection pooler configured with plan_cache_mode=force_generic_plan
// could all trigger it). Built via fmt.Sprintf from scope.KindStateSnapshot,
// not copy-pasted, so a rename of that constant cannot silently desync the
// SQL literal from the Go value it names -- see
// TestIngestionStoreListActiveStateSnapshotScopesReturnsBoundedPendingScopes
// for the regression proof. See the evidence doc's "Plan-mode proof" section
// for the full generic-plan measurement.
var listActiveStateSnapshotScopesLimitedQuery = fmt.Sprintf(`
SELECT scope.scope_id, scope.active_generation_id
FROM ingestion_scopes AS scope
WHERE scope.scope_kind = '%s'
  AND scope.active_generation_id IS NOT NULL
ORDER BY scope.scope_id ASC
LIMIT $1
`, scope.KindStateSnapshot)

// ListActiveStateSnapshotScopes implements
// projector.ActiveStateSnapshotScopeLister for
// projector.ConfigStateDriftCatchUpSweeper (issue #5593): it lists up to
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
