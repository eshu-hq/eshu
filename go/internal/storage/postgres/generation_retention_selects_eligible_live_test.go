// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"
)

// TestGenerationRetentionSelectsEligibleGenerationsAcrossScopesLive is P1 of
// arbiter ruling arb-7334.md's required proof table: 15 scopes ("blocked-*")
// sort first by scope_id and each own one old superseded generation whose
// rank (1) never clears MinSupersededGenerations (1), so none of them is
// ever eligible; 10 scopes ("zzzz-*") sort after and each own two old
// superseded generations, so their older (rank 2) generation is eligible.
//
// RED (E1): legacyGenerationRetentionCandidateQuery's locked_scopes only
// checks that a scope owns SOME old superseded generation, not that it owns
// an eligible one. With BatchGenerationLimit equal to the ineligible scope
// count, its scope_id ASC ordering fills the whole lock budget on the
// ineligible "blocked-*" scopes and finds zero eligible generations, even
// though ten exist.
//
// GREEN: the shipped store, same fixture and limit, prunes exactly the ten
// eligible generations and leaves every other row (the ineligible scopes'
// only generation, and the eligible scopes' newer, rank-1 generation) alone.
func TestGenerationRetentionSelectsEligibleGenerationsAcrossScopesLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const (
		minSuperseded = 1
		maxAge        = 7 * 24 * time.Hour
		blockedCount  = 15
		eligibleCount = 10
	)
	cutoff := now.Add(-maxAge)
	old := now.Add(-10 * 24 * time.Hour)
	newer := now.Add(-9 * 24 * time.Hour)

	var blockedGenerations, eligibleOlderGenerations, eligibleNewerGenerations []string
	for i := 0; i < blockedCount; i++ {
		scopeID := blockedScopeID(i)
		seedRetentionSelectionScope(t, ctx, database, scopeID)
		generationID := scopeID + "-g0"
		seedRetentionSelectionSupersededGeneration(t, ctx, database, scopeID, generationID, old)
		blockedGenerations = append(blockedGenerations, generationID)
	}
	for i := 0; i < eligibleCount; i++ {
		scopeID := eligibleScopeID(i)
		seedRetentionSelectionScope(t, ctx, database, scopeID)
		olderID, newerID := scopeID+"-g0", scopeID+"-g1"
		seedRetentionSelectionSupersededGeneration(t, ctx, database, scopeID, olderID, old)
		seedRetentionSelectionSupersededGeneration(t, ctx, database, scopeID, newerID, newer)
		eligibleOlderGenerations = append(eligibleOlderGenerations, olderID)
		eligibleNewerGenerations = append(eligibleNewerGenerations, newerID)
	}

	t.Run("red-legacy-query-starves-eligible-scopes", func(t *testing.T) {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		rows, err := tx.QueryContext(ctx, legacyGenerationRetentionCandidateQuery,
			cutoff, minSuperseded, blockedCount, []string{})
		if err != nil {
			t.Fatalf("legacy candidate query: %v", err)
		}
		defer func() { _ = rows.Close() }()
		var got int
		for rows.Next() {
			var scopeID, generationID, scopeKind string
			var supersededAt, observedAt time.Time
			if err := rows.Scan(&scopeID, &generationID, &scopeKind, &supersededAt, &observedAt); err != nil {
				t.Fatalf("scan legacy row: %v", err)
			}
			got++
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("legacy candidate query: %v", err)
		}
		if got != 0 {
			t.Fatalf("legacy candidate query returned %d rows, want 0 (E1: starved by ineligible scopes sorting first)", got)
		}
	})

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(ctx, retentionSelectionPolicy(minSuperseded, maxAge, blockedCount))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != eligibleCount {
		t.Fatalf("GenerationsPruned = %d, want %d", result.GenerationsPruned, eligibleCount)
	}

	remaining := remainingScopeGenerations(t, ctx, database, append(append(append([]string{}, blockedGenerations...), eligibleOlderGenerations...), eligibleNewerGenerations...))
	for _, id := range blockedGenerations {
		if !remaining[id] {
			t.Errorf("ineligible generation %s was pruned, want retained", id)
		}
	}
	for _, id := range eligibleNewerGenerations {
		if !remaining[id] {
			t.Errorf("rank-1 generation %s was pruned, want retained (not yet past MinSupersededGenerations)", id)
		}
	}
	for _, id := range eligibleOlderGenerations {
		if remaining[id] {
			t.Errorf("eligible generation %s was retained, want pruned", id)
		}
	}
}

func blockedScopeID(i int) string  { return retentionSelectionScopeID("blocked", i) }
func eligibleScopeID(i int) string { return retentionSelectionScopeID("zzzz", i) }

func retentionSelectionScopeID(prefix string, i int) string {
	digits := "0123456789"
	return prefix + "-" + string(digits[i/10]) + string(digits[i%10])
}
