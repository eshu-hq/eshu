// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// findInFlightNonMarkerClaimsQuery is set B of the two-read in-flight
// narrowing (#10972 D4b): findInFlightNonMarkerScopesQuery with each item's
// claim identity (last_attempt_at) beside its scope. It is derived from the
// shipped constant, so the row set cannot drift from the read the marker uses
// (TestInFlightClaimQueriesDeriveFromTheShippedInFlightQuery). Same index
// (fact_work_items_stage_domain_status_idx) and bound: the rows are the passes
// in flight at once.
var findInFlightNonMarkerClaimsQuery = strings.Replace(
	strings.Replace(findInFlightNonMarkerScopesQuery,
		"SELECT DISTINCT work.scope_id\n", "SELECT DISTINCT work.scope_id, work.last_attempt_at\n", 1),
	"ORDER BY work.scope_id", "ORDER BY work.scope_id, work.last_attempt_at", 1)

// inFlightActiveGenerationPredicate is the join arm that limits set B to the
// item at each scope's active generation.
const inFlightActiveGenerationPredicate = "\n AND scope.active_generation_id = work.generation_id"

// snapshotInFlightNonMarkerClaimsQuery is set A, read once when a findings
// pass's evidence load ends: set B without the active-generation arm, so an
// item claimed before its generation became active is named too. A wider A can
// only keep a flag. Derived from findInFlightNonMarkerClaimsQuery.
var snapshotInFlightNonMarkerClaimsQuery = strings.Replace(findInFlightNonMarkerClaimsQuery,
	inFlightActiveGenerationPredicate, "", 1)

// FindInFlightNonMarkerClaims implements set B of
// supplychaincore.InFlightClaimReader under the holder read's bounded
// read-only transaction and statement timeout.
func (s SupplyChainImpactFanOutStore) FindInFlightNonMarkerClaims(ctx context.Context, ownScopeID string) ([]supplychaincore.InFlightClaim, error) {
	return s.readInFlightClaims(ctx, findInFlightNonMarkerClaimsQuery, ownScopeID)
}

// SnapshotInFlightNonMarkerClaims implements set A of
// supplychaincore.InFlightClaimReader with the same transaction and timeout.
func (s SupplyChainImpactFanOutStore) SnapshotInFlightNonMarkerClaims(ctx context.Context, ownScopeID string) ([]supplychaincore.InFlightClaim, error) {
	return s.readInFlightClaims(ctx, snapshotInFlightNonMarkerClaimsQuery, ownScopeID)
}

// readInFlightClaims runs one claim read in a read-only transaction bounded by
// the holder-read statement timeout. A timeout maps to
// supplychaincore.ErrFanOutReadTimeout. A NULL last_attempt_at scans as the zero
// time, which never matches, so such an item is always flagged.
func (s SupplyChainImpactFanOutStore) readInFlightClaims(ctx context.Context, query, ownScopeID string) ([]supplychaincore.InFlightClaim, error) {
	if s.Beginner == nil {
		return nil, errors.New("supply chain impact fan-out beginner is required")
	}
	timeout := s.ReadTimeout
	if timeout <= 0 {
		timeout = supplyChainImpactFanOutReadTimeout
	}
	tx, err := s.Beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin supply chain impact in-flight claim read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SET TRANSACTION READ ONLY"); err != nil {
		return nil, fmt.Errorf("set supply chain impact in-flight claim read read-only: %w", err)
	}
	set, err := tx.QueryContext(ctx, setSupplyChainImpactFanOutReadTimeoutQuery, fmt.Sprintf("%dms", timeout.Milliseconds()))
	if err != nil {
		return nil, fmt.Errorf("set supply chain impact in-flight claim read timeout: %w", err)
	}
	_ = set.Close()
	rows, err := tx.QueryContext(ctx, query, ownScopeID)
	if err != nil {
		return nil, classifyFanOutReadError(err)
	}
	defer func() { _ = rows.Close() }()
	var claims []supplychaincore.InFlightClaim
	for rows.Next() {
		var (
			claim     supplychaincore.InFlightClaim
			claimedAt sql.NullTime
		)
		if err := rows.Scan(&claim.ScopeID, &claimedAt); err != nil {
			return nil, fmt.Errorf("scan supply chain impact in-flight claim: %w", err)
		}
		if claimedAt.Valid {
			claim.ClaimedAt = claimedAt.Time
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyFanOutReadError(err)
	}
	return claims, nil
}
