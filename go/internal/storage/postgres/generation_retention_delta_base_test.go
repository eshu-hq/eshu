// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestGenerationRetentionCandidateQueryProtectsDeltaBase pins the #7802
// guard in the all-scope candidate query: the delta_base definition finds
// each delta-active scope's newest full generation, eligible flags it and
// its newer chain as delta_protected, both stay out of the lock set and
// the prunable leg, and a lock-free leg reports them.
func TestGenerationRetentionCandidateQueryProtectsDeltaBase(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"delta_base AS (",
		"active.is_delta = true",
		"generation.is_delta = false",
		"AS delta_protected",
		"NOT eligible.delta_protected",
		"reported_delta_bases AS (",
		"WHERE eligible.delta_protected",
		"AND NOT eligible.uncovered",
		"true AS delta_protected",
	} {
		if !strings.Contains(generationRetentionCandidateQuery, want) {
			t.Errorf("candidate query missing delta-base guard %q", want)
		}
	}
}

// TestGenerationRetentionTargetedQueryProtectsDeltaBase pins the same #7802
// guard in the re-lock: a scope that turned delta-active after the
// selection still shields its base and chain at lock time.
func TestGenerationRetentionTargetedQueryProtectsDeltaBase(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"delta_base AS (",
		"AS delta_protected",
		"NOT provisional.delta_protected",
		"WHERE provisional.delta_protected",
		"AND NOT provisional.uncovered",
	} {
		if !strings.Contains(generationRetentionTargetedCandidateQuery, want) {
			t.Errorf("targeted candidate query missing delta-base guard %q", want)
		}
	}
}

// deltaBaseCandidateRow stages one candidate-query row; protected selects
// the #7802 report leg.
func deltaBaseCandidateRow(scope, generation string, supersededAt, observedAt time.Time, uncovered, protected bool) []any {
	return []any{scope, generation, "repository", supersededAt, observedAt, uncovered, "", false, "", protected}
}

// TestGenerationRetentionStoreSkipsDeltaProtectedCandidates proves a
// delta-protected row past the hard ceiling is counted as skipped and
// never pruned, while an ordinary past-ceiling generation in the same
// batch still prunes.
func TestGenerationRetentionStoreSkipsDeltaProtectedCandidates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	old := now.Add(-100 * 24 * time.Hour)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			deltaBaseCandidateRow("scope-delta", "generation-base", old, old.Add(-time.Hour), false, true),
			deltaBaseCandidateRow("scope-full", "generation-old", old, old.Add(-time.Hour), false, false),
		},
		execResults: []sql.Result{
			fakeResult{},           // retention event insert
			fakeRowsAffected{n: 0}, // shared_projection_intents delete
			fakeRowsAffected{n: 0}, // shared_projection_unroutable_intents delete
			fakeRowsAffected{n: 0}, // content_file_references prune
			fakeResult{},           // infra inventory repository locks
			fakeRowsAffected{n: 0}, // content_entities prune
			fakeRowsAffected{n: 0}, // infra_resource_entities orphan delete
			fakeRowsAffected{n: 0}, // content_files prune
			fakeRowsAffected{n: 1}, // scope_generations delete cascades owned rows
		},
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         7 * 24 * time.Hour,
		HardMaxSupersededAge:     90 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "7802-delta-base",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 1 {
		t.Errorf("GenerationsPruned = %d, want 1 (only the unprotected generation)", result.GenerationsPruned)
	}
	if got := result.Skipped["delta_base"]; got != 1 {
		t.Errorf("Skipped[delta_base] = %d, want 1", got)
	}
	for _, exec := range database.execs {
		for _, arg := range exec.args {
			if ids, ok := arg.([]string); ok {
				for _, id := range ids {
					if id == "generation-base" {
						t.Errorf("protected base generation reached a prune statement")
					}
				}
			}
		}
	}
}

// TestGenerationRetentionStoreAcceptsLegacySixColumnCandidateRows proves
// fixtures staged before the #7981 identity triple and the #7802 flag still
// scan as unprotected and prune, so the trailing columns stay backward
// compatible.
func TestGenerationRetentionStoreAcceptsLegacySixColumnCandidateRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	old := now.Add(-100 * 24 * time.Hour)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{{
			"scope-legacy",
			"generation-legacy",
			"repository",
			old,
			old.Add(-time.Hour),
			false,
		}},
		execResults: []sql.Result{
			fakeResult{},           // retention event insert
			fakeRowsAffected{n: 0}, // shared_projection_intents delete
			fakeRowsAffected{n: 0}, // shared_projection_unroutable_intents delete
			fakeRowsAffected{n: 0}, // content_file_references prune
			fakeResult{},           // infra inventory repository locks
			fakeRowsAffected{n: 0}, // content_entities prune
			fakeRowsAffected{n: 0}, // infra_resource_entities orphan delete
			fakeRowsAffected{n: 0}, // content_files prune
			fakeRowsAffected{n: 1}, // scope_generations delete cascades owned rows
		},
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         7 * 24 * time.Hour,
		HardMaxSupersededAge:     90 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "7802-delta-base-legacy",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 1 {
		t.Errorf("GenerationsPruned = %d, want 1 (six-column row prunes as unprotected)", result.GenerationsPruned)
	}
	if got := result.Skipped["delta_base"]; got != 0 {
		t.Errorf("Skipped[delta_base] = %d, want 0", got)
	}
}

// TestGenerationRetentionRelockDropsDeltaProtected proves the re-lock drops
// a member that flipped delta-protected after the selection: it is counted
// as skipped instead of pruned.
func TestGenerationRetentionRelockDropsDeltaProtected(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	old := now.Add(-100 * 24 * time.Hour)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			deltaBaseCandidateRow("scope-flip", "generation-flip", old, old.Add(-time.Hour), false, false),
		},
		targetedLockRows: [][]any{
			deltaBaseCandidateRow("scope-flip", "generation-flip", old, old.Add(-time.Hour), false, true),
		},
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         7 * 24 * time.Hour,
		HardMaxSupersededAge:     90 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "7802-delta-base-relock",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 0 {
		t.Errorf("GenerationsPruned = %d, want 0 (flipped member leaves the batch)", result.GenerationsPruned)
	}
	if got := result.Skipped["delta_base"]; got != 1 {
		t.Errorf("Skipped[delta_base] = %d, want 1", got)
	}
}

// deltaBaseIdentityRow stages one ten-column candidate row carrying the
// #7981 identity triple beside the #7802 flag, in the production column
// order of both candidate statements.
func deltaBaseIdentityRow(scope, generation, sha string, supersededAt, observedAt time.Time, protected bool) []any {
	return []any{scope, generation, "repository", supersededAt, observedAt, false, sha, false, "hint", protected}
}

// TestGenerationRetentionCandidateScanCarriesDeltaProtected pins the
// merged column order: the identity triple precedes delta_protected, and a
// nine-column row staged before the flag still scans unprotected.
func TestGenerationRetentionCandidateScanCarriesDeltaProtected(t *testing.T) {
	t.Parallel()

	now := testSameSHAClock()
	row := deltaBaseIdentityRow("scope-a", "gen-base", "sha-a", now, now, true)
	got, err := scanRetentionCandidates(&queueFakeRows{rows: [][]any{row}})
	if err != nil {
		t.Fatalf("scan ten-column row: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	if candidate := got[0]; !candidate.deltaProtected || candidate.sourceCommitSHA != "sha-a" || candidate.freshnessHint != "hint" {
		t.Fatalf("candidate = (%q, %q, protected %v), want (sha-a, hint, true)",
			candidate.sourceCommitSHA, candidate.freshnessHint, candidate.deltaProtected)
	}

	nineColumn := row[:9]
	got, err = scanRetentionCandidates(&queueFakeRows{rows: [][]any{nineColumn}})
	if err != nil {
		t.Fatalf("scan nine-column row: %v", err)
	}
	if len(got) != 1 || got[0].deltaProtected {
		t.Fatalf("nine-column candidates = %+v, want one unprotected row", got)
	}
}

// TestGenerationRetentionDeltaBaseNeverCountedAsWitnessRetained proves a
// delta-protected row that shares a same-SHA identity with older prunable
// copies is tallied once, as delta_base: the selection filters it out before
// the #7981 witness runs, so it is never also retained as the newest copy.
// The two older copies still go through the witness, which retains the newer
// of them and prunes the older.
func TestGenerationRetentionDeltaBaseNeverCountedAsWitnessRetained(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	old := now.Add(-100 * 24 * time.Hour)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			deltaBaseIdentityRow("scope-delta", "generation-copy-a", "sha-x", old, old.Add(-3*time.Hour), false),
			deltaBaseIdentityRow("scope-delta", "generation-copy-b", "sha-x", old.Add(time.Hour), old.Add(-2*time.Hour), false),
			deltaBaseIdentityRow("scope-delta", "generation-base", "sha-x", old.Add(2*time.Hour), old.Add(-time.Hour), true),
		},
		execResults: generationRetentionExecResults(1, 0, 0, 0, 0, 0, 0, 1),
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         7 * 24 * time.Hour,
		HardMaxSupersededAge:     90 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "7802-delta-base-witness",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if got := result.Skipped["delta_base"]; got != 1 {
		t.Errorf("Skipped[delta_base] = %d, want 1 (the protected base)", got)
	}
	if got := result.Skipped[sameSHANewestSkippedReason]; got != 1 {
		t.Errorf("Skipped[%s] = %d, want 1 (the newer unprotected copy only)", sameSHANewestSkippedReason, got)
	}
	if result.GenerationsPruned != 1 {
		t.Errorf("GenerationsPruned = %d, want 1 (the oldest copy)", result.GenerationsPruned)
	}
	var deleted []string
	for _, exec := range database.execs {
		if strings.Contains(exec.query, "DELETE FROM scope_generations") {
			deleted, _ = exec.args[0].([]string)
		}
	}
	if !slices.Equal(deleted, []string{"generation-copy-a"}) {
		t.Errorf("generation delete ids = %v, want [generation-copy-a]", deleted)
	}
}

// TestGenerationRetentionChunkTargetFlippedDeltaProtectedCountsOnce proves
// the #7981 chunked re-lock honors the #7802 guard: an over-limit chunk
// target whose scope turned delta-active after the selection is reported
// delta-protected at lock time, so it is tallied once as delta_base, drains
// no chunk, and never reaches a fact or generation delete, while the normal
// batch beside it still prunes.
func TestGenerationRetentionChunkTargetFlippedDeltaProtectedCountsOnce(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	old := now.Add(-100 * 24 * time.Hour)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			deltaBaseCandidateRow("scope-flip", "generation-huge", old, old.Add(-time.Hour), false, false),
			deltaBaseCandidateRow("scope-full", "generation-small", old.Add(time.Hour), old, false, false),
		},
		targetedLockRows: [][]any{
			deltaBaseCandidateRow("scope-flip", "generation-huge", old, old.Add(-time.Hour), false, true),
			deltaBaseCandidateRow("scope-full", "generation-small", old.Add(time.Hour), old, false, false),
		},
		prescreenRows: [][]any{
			{"generation-huge", int64(101)},
			{"generation-small", int64(2)},
		},
		countRows:   [][]any{{"generation-small", "fact_records", int64(2)}},
		execResults: generationRetentionExecResults(1, 0, 0, 0, 0, 0, 0, 1),
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         7 * 24 * time.Hour,
		HardMaxSupersededAge:     90 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "7802-delta-base-chunk",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if got := result.Skipped["delta_base"]; got != 1 {
		t.Errorf("Skipped[delta_base] = %d, want 1 (the flipped chunk target)", got)
	}
	if got := result.Skipped[generationRetentionChunkedProgressSkippedReason]; got != 0 {
		t.Errorf("Skipped[%s] = %d, want 0 (a protected target drains no chunk)",
			generationRetentionChunkedProgressSkippedReason, got)
	}
	if result.GenerationsPruned != 1 {
		t.Errorf("GenerationsPruned = %d, want 1 (the normal batch member)", result.GenerationsPruned)
	}
	for _, exec := range database.execs {
		if strings.Contains(exec.query, "retention: over-limit chunk") {
			t.Errorf("chunk statement ran for a delta-protected target: %q", exec.query)
		}
		for _, arg := range exec.args {
			if ids, ok := arg.([]string); ok && slices.Contains(ids, "generation-huge") {
				t.Errorf("protected chunk target reached statement %q", exec.query)
			}
		}
	}
}
