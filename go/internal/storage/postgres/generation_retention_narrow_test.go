// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

// statementLabel names a retention statement for the sequence assertions.
func statementLabel(statement string) string {
	switch {
	case strings.HasPrefix(statement, "SET LOCAL "):
		return "work_mem"
	case strings.HasPrefix(statement, "SAVEPOINT "):
		return "savepoint"
	case strings.HasPrefix(statement, "RELEASE SAVEPOINT "):
		return "release"
	case strings.HasPrefix(statement, "ROLLBACK TO SAVEPOINT "):
		return "rollback_to_savepoint"
	case strings.Contains(statement, "generation_retention_key_indexes"):
		return "key_index_check"
	case strings.Contains(statement, "retention: targeted candidate lock"):
		return "targeted_lock"
	case strings.Contains(statement, "retention: own-fact pre-screen"):
		return "prescreen"
	case strings.Contains(statement, "ranked_superseded_generations"):
		return "candidates"
	case strings.Contains(statement, "generation_retention_row_counts"):
		return "count"
	case strings.Contains(statement, "INSERT INTO generation_retention_events"):
		return "event"
	case strings.Contains(statement, "del_activations"):
		return "ledger_delete"
	case strings.Contains(statement, "doomed AS MATERIALIZED"):
		return "ledger_count"
	default:
		return "prune"
	}
}

// deletedGenerations returns the ids the scope_generations delete named.
func deletedGenerations(database *generationRetentionFakeDB) []string {
	for _, exec := range database.execs {
		if strings.Contains(exec.query, "DELETE FROM scope_generations") {
			ids, _ := exec.args[0].([]string)
			return ids
		}
	}
	return nil
}

func statementLabels(statements []string) []string {
	labels := make([]string, 0, len(statements))
	for _, statement := range statements {
		labels = append(labels, statementLabel(statement))
	}
	return labels
}

// narrowFake is candidate a (5 fact rows) and candidate b (5 fact rows);
// with ledgerRows > 0, a also carries that many link-delta rows.
func narrowFake(now time.Time, ledgerRows int64) *generationRetentionFakeDB {
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			recheckCandidate("scope-a", "a", now, 14), recheckCandidate("scope-b", "b", now, 13),
		},
		countRows: [][]any{{"a", "fact_records", int64(5)}, {"b", "fact_records", int64(5)}},
	}
	if ledgerRows > 0 {
		database.ledgerCountRows = [][]any{{"a", "changed_since_link_deltas", ledgerRows}}
	}
	return database
}

// TestGenerationRetentionNarrowsTheLockSetForAnOverLimitBatch is PC1 (a) of
// arbiter ruling arb-7127-3d-c, kept through #7334's unification: every batch
// now releases the selection's locks (ROLLBACK TO SAVEPOINT), plans unlocked
// (pre-screen, then the row counts), re-locks only its selected set,
// recounts under that lock, then runs the unchanged prune sequence. The
// over-limit batch below is the PC1 (a) instance: admitted alone over
// BatchRowLimit on ledger rows only, narrowed to its own scope and
// generation. RED: without the rollback the batch keeps the selection's lock
// set through its counts.
func TestGenerationRetentionNarrowsTheLockSetForAnOverLimitBatch(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	run := func(database *generationRetentionFakeDB) GenerationRetentionResult {
		t.Helper()
		store := NewGenerationRetentionStore(database)
		store.Now = func() time.Time { return now }
		result, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(60))
		if err != nil {
			t.Fatalf("PruneSupersededGenerations() error = %v", err)
		}
		return result
	}

	normal := narrowFake(now, 0)
	normalResult := run(normal)
	normalLabels := statementLabels(normal.statements)
	wantNormalHead := []string{
		"work_mem", "key_index_check", "savepoint", "candidates",
		"rollback_to_savepoint", "prescreen", "count", "ledger_count",
		"targeted_lock", "count", "ledger_count",
	}
	if got := deletedGenerations(normal); !slices.Equal(got, []string{"a", "b"}) || normalResult.LockedScopeRows != 2 ||
		!slices.Equal(normalLabels[:len(wantNormalHead)], wantNormalHead) {
		t.Fatalf("normal batch: deleted %v with %d locked scope rows, statements %v; want [a b], 2, after %v",
			got, normalResult.LockedScopeRows, normalLabels, wantNormalHead)
	}

	over := narrowFake(now, 90)
	overResult := run(over)
	overLabels := statementLabels(over.statements)
	// work_mem, the #7279 key-index check, SAVEPOINT, the candidate query,
	// the rollback, the pre-screen, one or more count pairs (the first count
	// and the limit re-checks), then the re-lock and its recount.
	targetedAt := slices.Index(overLabels, "targeted_lock")
	if targetedAt < 8 || !slices.Equal(overLabels[:6], []string{"work_mem", "key_index_check", "savepoint", "candidates", "rollback_to_savepoint", "prescreen"}) {
		t.Fatalf("over-limit batch statements = %v, want work_mem, key_index_check, savepoint, candidates, rollback, prescreen, counts, re-lock", overLabels)
	}
	for _, label := range overLabels[6:targetedAt] {
		if label != "count" && label != "ledger_count" {
			t.Fatalf("over-limit batch ran %q between its pre-screen and its re-lock: %v", label, overLabels)
		}
	}
	wantOverHead := append(slices.Clone(overLabels[:targetedAt]), "targeted_lock", "count", "ledger_count")
	if got := deletedGenerations(over); !slices.Equal(got, []string{"a"}) || overResult.RowsOverLimit != 35 || overResult.LockedScopeRows != 1 {
		t.Fatalf("over-limit batch: deleted %v over by %d with %d locked scope rows, want [a] over by 35 with 1",
			got, overResult.RowsOverLimit, overResult.LockedScopeRows)
	}
	if len(overLabels) < len(wantOverHead) || !slices.Equal(overLabels[:len(wantOverHead)], wantOverHead) {
		t.Fatalf("over-limit batch statements = %v, want to start %v", overLabels, wantOverHead)
	}
	// After the re-lock, the prune sequence is the base one: one event per
	// pruned generation, then the same deletes in the same order.
	withoutEvents := func(labels []string) []string {
		return slices.DeleteFunc(slices.Clone(labels), func(l string) bool { return l == "event" })
	}
	overTail, normalTail := overLabels[len(wantOverHead):], normalLabels[len(wantNormalHead):]
	if !slices.Equal(withoutEvents(overTail), withoutEvents(normalTail)) ||
		len(overTail)-len(withoutEvents(overTail)) != 1 || len(normalTail)-len(withoutEvents(normalTail)) != 2 {
		t.Fatalf("over-limit prune sequence %v differs from the normal one %v beyond one event per generation", overTail, normalTail)
	}
	for _, query := range over.queries {
		if strings.Contains(query.query, "retention: targeted candidate lock") {
			scopes, _ := query.args[3].([]string)
			generations, _ := query.args[4].([]string)
			if !slices.Equal(scopes, []string{"scope-a"}) || !slices.Equal(generations, []string{"a"}) {
				t.Fatalf("targeted lock args = %v, want [scope-a] and [a]", query.args)
			}
		}
	}
}

// TestGenerationRetentionNarrowedCandidateTakenElsewhere is PC1 (c) in the
// database double: when the targeted lock returns no row (another session
// took the candidate after the rollback), the pass commits, prunes nothing,
// writes no event and returns no error.
func TestGenerationRetentionNarrowedCandidateTakenElsewhere(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	database := narrowFake(now, 90)
	database.targetedLockMiss = true
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(60))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if got := deletedGenerations(database); got != nil || result.RowsOverLimit != 0 {
		t.Fatalf("deleted %v over by %d, want nothing", got, result.RowsOverLimit)
	}
	labels := statementLabels(database.statements)
	if !slices.Equal(labels[len(labels)-1:], []string{"targeted_lock"}) || slices.Contains(labels, "prune") || slices.Contains(labels, "ledger_delete") {
		t.Fatalf("statements = %v, want the pass to end at the targeted lock with no prune or event", labels)
	}
}

// TestGenerationRetentionRelockDropsMissedMembersAndPrunesTheRest is #7334
// fix 1: when the targeted re-lock returns only part of the provisionally
// selected set (another session took the rest after the rollback), the pass
// silently drops the missed members — no skip reason, no event — recounts
// the locked subset and prunes it.
func TestGenerationRetentionRelockDropsMissedMembersAndPrunesTheRest(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	database := narrowFake(now, 0)
	database.targetedLockRows = [][]any{recheckCandidate("scope-b", "b", now, 13)}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(60))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if got := deletedGenerations(database); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("deleted %v, want [b]: the re-lock missed a, so only b prunes", got)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want no skip for the dropped member", result.Skipped)
	}
	if result.LockedScopeRows != 1 {
		t.Fatalf("LockedScopeRows = %d, want 1 (scope-b only)", result.LockedScopeRows)
	}
}
