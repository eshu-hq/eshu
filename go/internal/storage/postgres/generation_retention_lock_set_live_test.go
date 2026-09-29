// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestGenerationRetentionLockSetMatchesEligibleScopesLive is P2: the scope
// lock set a second session observes equals exactly the scopes that own an
// eligible generation; a scope whose only old generation has live work is
// not locked. Five scopes each own one eligible generation; one more scope's
// only old generation would be eligible by rank and age alone but has a
// running fact_work_items row.
func TestGenerationRetentionLockSetMatchesEligibleScopesLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	old := now.Add(-10 * 24 * time.Hour)

	var eligibleScopes []string
	for i := 0; i < 5; i++ {
		scopeID := retentionSelectionScopeID("lockset", i)
		seedRetentionSelectionScope(t, ctx, database, scopeID)
		seedRetentionSelectionSupersededGeneration(t, ctx, database, scopeID, scopeID+"-g0", old)
		eligibleScopes = append(eligibleScopes, scopeID)
	}
	liveWorkScope := "lockset-live"
	seedRetentionSelectionScope(t, ctx, database, liveWorkScope)
	liveWorkGeneration := liveWorkScope + "-g0"
	seedRetentionSelectionSupersededGeneration(t, ctx, database, liveWorkScope, liveWorkGeneration, old)
	seedRetentionSelectionLiveWork(t, ctx, database, liveWorkScope, liveWorkGeneration)

	allScopes := append(append([]string{}, eligibleScopes...), liveWorkScope)

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, generationRetentionCandidateQuery, now.Add(-7*24*time.Hour), 0, 100)
	if err != nil {
		t.Fatalf("candidate query: %v", err)
	}
	candidateScopes := map[string]bool{}
	for rows.Next() {
		var scopeID, generationID, scopeKind string
		var supersededAt, observedAt time.Time
		if err := rows.Scan(&scopeID, &generationID, &scopeKind, &supersededAt, &observedAt); err != nil {
			_ = rows.Close()
			t.Fatalf("scan candidate: %v", err)
		}
		candidateScopes[scopeID] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("candidate query: %v", err)
	}
	_ = rows.Close()

	// tx stays open (uncommitted, unrolled-back until the deferred Rollback)
	// so its FOR UPDATE OF scope SKIP LOCKED locks are still held while a
	// second session probes them.
	locked := probeLockedScopeSet(t, ctx, database, allScopes)

	for _, scopeID := range eligibleScopes {
		if !locked[scopeID] {
			t.Errorf("eligible scope %s not locked, want locked", scopeID)
		}
		if !candidateScopes[scopeID] {
			t.Errorf("eligible scope %s produced no candidate row", scopeID)
		}
	}
	if locked[liveWorkScope] {
		t.Errorf("scope %s (only generation has live work) is locked, want not locked", liveWorkScope)
	}
	if candidateScopes[liveWorkScope] {
		t.Errorf("scope %s (only generation has live work) produced a candidate row", liveWorkScope)
	}
	if got, want := len(locked), len(eligibleScopes); got != want {
		t.Fatalf("locked scope count = %d, want %d: %v", got, want, locked)
	}
}

// probeLockedScopeSet opens a second transaction and attempts FOR UPDATE SKIP
// LOCKED on every id in scopeIDs; the ones it cannot lock (skipped, so absent
// from its result) are the ones another session already holds.
func probeLockedScopeSet(t *testing.T, ctx context.Context, database *sql.DB, scopeIDs []string) map[string]bool {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("probe begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx,
		`SELECT scope_id FROM ingestion_scopes WHERE scope_id = ANY($1::text[]) FOR UPDATE SKIP LOCKED`, scopeIDs)
	if err != nil {
		t.Fatalf("probe lock: %v", err)
	}
	defer func() { _ = rows.Close() }()
	unlocked := map[string]bool{}
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			t.Fatalf("probe scan: %v", err)
		}
		unlocked[scopeID] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("probe lock: %v", err)
	}
	locked := map[string]bool{}
	for _, scopeID := range scopeIDs {
		if !unlocked[scopeID] {
			locked[scopeID] = true
		}
	}
	return locked
}
