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
		if strings.Contains(statement, "FOR UPDATE") || strings.Contains(statement, "DELETE") ||
			strings.Contains(statement, "generation_retention_row_counts") {
			t.Fatalf("refused cycle still ran %q", strings.Fields(statement)[0])
		}
	}
	if len(database.execs) != 0 {
		t.Errorf("refused cycle issued %d writes, want 0", len(database.execs))
	}
}

// TestGenerationRetentionChecksKeyIndexesBeforeLocking pins the order: the
// catalog check runs after the transaction-local work_mem setting and before the
// candidate statement takes any scope lock. Only the selection savepoint
// (#7127 PR-3d), which takes no lock, may sit between the two.
func TestGenerationRetentionChecksKeyIndexesBeforeLocking(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }
	if _, err := store.PruneSupersededGenerations(context.Background(), DefaultGenerationRetentionPolicy()); err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if len(database.statements) < 3 {
		t.Fatalf("statements = %d, want the setting, the index check and the candidate lock", len(database.statements))
	}
	if !strings.Contains(database.statements[1], "generation_retention_key_indexes") {
		t.Errorf("second statement = %q, want the key-index check", strings.Fields(database.statements[1])[0])
	}
	candidateAt := slices.IndexFunc(database.statements, func(statement string) bool {
		return strings.Contains(statement, "ranked_superseded_generations")
	})
	if candidateAt < 2 {
		t.Fatalf("candidate lock at statement %d, want it after the key-index check", candidateAt)
	}
	for _, statement := range database.statements[2:candidateAt] {
		if !strings.HasPrefix(statement, "SAVEPOINT ") {
			t.Errorf("statement %q runs between the key-index check and the candidate lock, want only the savepoint", strings.Fields(statement)[0])
		}
	}
}
