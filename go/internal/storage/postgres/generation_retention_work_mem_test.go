// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestGenerationRetentionSetsTransactionLocalWorkMemFirst pins that the
// retention transaction raises work_mem before its first statement. Every
// statement, the row count included, then plans its aggregate against a memory
// budget that keeps it hash-based on a server left at the 4MB default, and the
// setting stays transaction-local (#6809).
func TestGenerationRetentionSetsTransactionLocalWorkMemFirst(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{{
			"scope-old", "generation-old", "repository",
			now.Add(-10 * 24 * time.Hour), now.Add(-11 * 24 * time.Hour),
		}},
		countRows: [][]any{{"generation-old", "fact_records", int64(1)}},
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	if _, err := store.PruneSupersededGenerations(context.Background(), GenerationRetentionPolicy{
		MinSupersededGenerations: 1,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "test-revision",
	}); err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if len(database.statements) == 0 {
		t.Fatal("the retention transaction issued no statements")
	}
	if got, want := database.statements[0], "SET LOCAL work_mem = '64MB'"; got != want {
		t.Fatalf("first statement in the retention transaction = %q, want %q", got, want)
	}
	for _, statement := range database.statements[1:] {
		if strings.HasPrefix(statement, "SET ") && !strings.HasPrefix(statement, "SET LOCAL ") {
			t.Fatalf("session-level setting %q would outlive the retention transaction", statement)
		}
	}
}
