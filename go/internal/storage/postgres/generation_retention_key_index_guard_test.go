// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestGenerationRetentionRefusesCycleWithoutKeyIndexes pins the #7279
// precondition: when either key index is missing or invalid the store refuses
// the whole cycle before it locks a scope or runs a probe statement. Without the
// index every NOT EXISTS probe falls back to scanning fact_records once per
// candidate key, the #6809 cliff, while holding the scope locks.
func TestGenerationRetentionRefusesCycleWithoutKeyIndexes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		missingKeyIndexes: []string{"fact_records_file_key_idx"},
		candidateRows: [][]any{{
			"scope-old", "generation-old", "repository",
			now.Add(-10 * 24 * time.Hour), now.Add(-11 * 24 * time.Hour),
			false,
		}},
		countRows: [][]any{{"generation-old", "fact_records", int64(1)}},
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), DefaultGenerationRetentionPolicy())
	if !errors.Is(err, ErrGenerationRetentionKeyIndexUnavailable) {
		t.Fatalf("PruneSupersededGenerations() error = %v, want ErrGenerationRetentionKeyIndexUnavailable", err)
	}
	if !strings.Contains(err.Error(), "fact_records_file_key_idx") {
		t.Errorf("refusal %q does not name the missing index", err)
	}
	if result.GenerationsPruned != 0 {
		t.Errorf("GenerationsPruned = %d, want 0 on refusal", result.GenerationsPruned)
	}
	for _, statement := range database.statements {
		// FOR UPDATE OF is the strong lock; the pre-deletes' weakened FOR
		// NO KEY UPDATE reads never take it.
		if strings.Contains(statement, "FOR UPDATE OF") || strings.Contains(statement, "DELETE") ||
			strings.Contains(statement, "generation_retention_row_counts") {
			t.Fatalf("refused cycle still ran %q", strings.Fields(statement)[0])
		}
	}
	if len(database.execs) != 0 {
		t.Errorf("refused cycle issued %d writes, want 0", len(database.execs))
	}
}

// TestGenerationRetentionChecksKeyIndexesBeforeLocking pins the order: no
// statement takes a strong (FOR UPDATE OF) scope lock before its own
// key-index check. Each phase checks first: the search and chunk
// pre-deletes check on the pool before their unlocked reads, and the prune
// transaction checks after its work_mem setting and before the candidate
// statement. Only the selection savepoint (#7127 PR-3d), which takes no
// lock, may sit between the prune's check and its lock.
func TestGenerationRetentionChecksKeyIndexesBeforeLocking(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }
	if _, err := store.PruneSupersededGenerations(context.Background(), DefaultGenerationRetentionPolicy()); err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	// No strong (FOR UPDATE OF) lock runs before the prune transaction's
	// own check: the pre-deletes' reads that precede it are weakened to
	// FOR NO KEY UPDATE.
	checkAt := slices.IndexFunc(database.statements, func(statement string) bool {
		return strings.Contains(statement, "generation_retention_key_indexes")
	})
	if checkAt < 0 {
		t.Fatal("no key-index check ran in the prune transaction")
	}
	for i, statement := range database.statements[:checkAt] {
		if strings.Contains(statement, "FOR UPDATE OF") {
			t.Fatalf("strong lock at statement %d runs before the key-index check at %d", i, checkAt)
		}
	}
	// The prune's own selection follows its check with only the savepoint
	// between them.
	postCheck := database.statements[checkAt+1:]
	selectionAt := slices.IndexFunc(postCheck, func(statement string) bool {
		return strings.Contains(statement, "ranked_superseded_generations")
	})
	if selectionAt < 0 {
		t.Fatal("no candidate selection ran after the key-index check")
	}
	for _, statement := range postCheck[:selectionAt] {
		if !strings.HasPrefix(statement, "SAVEPOINT ") {
			t.Errorf("statement %q runs between the key-index check and the candidate selection, want only the savepoint", strings.Fields(statement)[0])
		}
	}
	// The pre-deletes' own checks run on the pool before any of their
	// statements.
	checks := 0
	for _, statement := range database.predeleteStatements {
		if strings.Contains(statement, "generation_retention_key_indexes") {
			checks++
		}
	}
	if checks != 2 {
		t.Fatalf("pool key-index checks = %d, want 2 (search pre-delete and chunk predrain)", checks)
	}
}
