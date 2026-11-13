// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestGenerationRetentionLockedRecountHasItsOwnPhase is the #10020
// attribution split: the locked recount under the targeted re-lock and the
// unlocked planning counts must be separately observable. Before the split
// every count accrued to count_rows, so the aggregated phase could evidence
// neither the 6.1 s planning bar nor the 2.0 s locked-recount bar; planning
// PASS hid behind recount cost and vice versa. RED: the locked recount's
// key is absent and its time hides inside count_rows.
func TestGenerationRetentionLockedRecountHasItsOwnPhase(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{{
			"scope-old",
			"generation-old",
			"repository",
			now.Add(-10 * 24 * time.Hour),
			now.Add(-11 * 24 * time.Hour),
			false,
		}},
		countRows: [][]any{
			{"generation-old", "fact_records", int64(3)},
			{"generation-old", "fact_work_items", int64(1)},
			{"generation-old", "shared_projection_intents", int64(2)},
		},
		recountRows: [][]any{
			{"generation-old", "fact_records", int64(3)},
			{"generation-old", "fact_work_items", int64(1)},
			{"generation-old", "shared_projection_intents", int64(2)},
		},
		execResults: []sql.Result{
			fakeResult{},           // retention event insert
			fakeRowsAffected{n: 2}, // shared_projection_intents delete
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
		MinSupersededGenerations: 1,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "test-revision",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1", result.GenerationsPruned)
	}
	if _, ok := result.PhaseDurations[GenerationRetentionPhaseCountRows]; !ok {
		t.Fatal("PhaseDurations has no count_rows phase for the unlocked planning counts")
	}
	if _, ok := result.PhaseDurations[GenerationRetentionPhaseLockedRecount]; !ok {
		t.Fatal("PhaseDurations has no locked_recount phase for the recount under the targeted re-lock")
	}
}
