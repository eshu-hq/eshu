// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGenerationRetentionStoreRowLimitSkipStopsAtSearchCap(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	candidates := generationRetentionCandidateRows(40, now)
	db := &generationRetentionFakeDB{
		candidateRows: candidates,
		countRows: generationRetentionCountRows(candidates, map[string]int64{
			"fact_records": 101,
		}),
	}
	store := NewGenerationRetentionStore(db)
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
	if result.GenerationsPruned != 0 {
		t.Fatalf("GenerationsPruned = %d, want 0", result.GenerationsPruned)
	}
	if got, want := result.Skipped["row_limit"], generationRetentionSkipSearchLimit(10); got != want {
		t.Fatalf("Skipped[row_limit] = %d, want %d", got, want)
	}
	if len(result.RowsPruned) != 0 {
		t.Fatalf("RowsPruned = %#v, want empty for skipped backlog", result.RowsPruned)
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec count = %d, want 0 for skipped backlog", len(db.execs))
	}

	var candidateQueries int
	var countQueries int
	var excludedLengths []int
	for _, query := range db.queries {
		switch {
		case strings.Contains(query.query, "ranked_superseded_generations"):
			candidateQueries++
			excluded, ok := query.args[3].([]string)
			if !ok {
				t.Fatalf("candidate query exclusion arg type = %T, want []string", query.args[3])
			}
			excludedLengths = append(excludedLengths, len(excluded))
		case strings.Contains(query.query, "generation_retention_row_counts"):
			countQueries++
		}
	}
	if got, want := candidateQueries, 4; got != want {
		t.Fatalf("candidate query count = %d, want %d", got, want)
	}
	if got, want := countQueries, 4; got != want {
		t.Fatalf("row-count query count = %d, want %d", got, want)
	}
	wantExcluded := []int{0, 10, 20, 30}
	if !reflect.DeepEqual(excludedLengths, wantExcluded) {
		t.Fatalf("candidate exclusion lengths = %#v, want %#v", excludedLengths, wantExcluded)
	}
}
