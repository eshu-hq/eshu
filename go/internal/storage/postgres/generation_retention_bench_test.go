// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func BenchmarkGenerationRetentionStoreLargeFixture(b *testing.B) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	candidates := generationRetentionCandidateRows(100, now)
	countRows := generationRetentionCountRows(candidates, map[string]int64{
		"fact_records":                        500,
		"fact_work_items":                     1,
		"fact_replay_events":                  1,
		"semantic_extraction_jobs":            2,
		"shared_projection_acceptance":        3,
		"graph_projection_phase_state":        3,
		"graph_projection_phase_repair_queue": 1,
		"iac_reachability":                    5,
		"shared_projection_intents":           4,
	})
	policy := GenerationRetentionPolicy{
		MinSupersededGenerations: 1,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     100,
		BatchRowLimit:            1_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "benchmark",
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		execResults := generationRetentionExecResults(len(candidates), 400, 5, 20, 20, 20, 100)
		scriptedExecs := len(execResults)
		db := &generationRetentionFakeDB{
			candidateRows: candidates,
			countRows:     countRows,
			execResults:   execResults,
		}
		store := NewGenerationRetentionStore(db)
		store.Now = func() time.Time { return now }
		result, err := store.PruneSupersededGenerations(context.Background(), policy)
		if err != nil {
			b.Fatalf("PruneSupersededGenerations() error = %v", err)
		}
		// Check the script alignment BEFORE the row counts. A statement added to
		// PruneSupersededGenerations without a matching result here runs off the
		// end of the script into the fakeResult{} fallback, which reports 1 row
		// rather than failing; asserting the count first would report that as an
		// unexplained "GenerationsPruned = 1".
		if len(db.execs) != scriptedExecs {
			b.Fatalf(
				"exec count = %d, want %d: generationRetentionExecResults is positional, "+
					"so add the new statement's result in PruneSupersededGenerations order",
				len(db.execs), scriptedExecs,
			)
		}
		if result.GenerationsPruned != 100 {
			b.Fatalf("GenerationsPruned = %d, want 100", result.GenerationsPruned)
		}
	}
}

func generationRetentionCandidateRows(count int, now time.Time) [][]any {
	rows := make([][]any, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, []any{
			"scope-old",
			"generation-old-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)),
			"repository",
			now.Add(time.Duration(-10-i) * 24 * time.Hour),
			now.Add(time.Duration(-11-i) * 24 * time.Hour),
		})
	}
	return rows
}

func generationRetentionCountRows(candidates [][]any, counts map[string]int64) [][]any {
	rows := make([][]any, 0, len(candidates)*len(counts))
	for _, candidate := range candidates {
		generationID, _ := candidate[1].(string)
		for tableName, count := range counts {
			rows = append(rows, []any{generationID, tableName, count})
		}
	}
	return rows
}

// generationRetentionExecResults scripts the fake's exec results POSITIONALLY:
// eventCount per-generation event inserts, then one result per batch-level
// delete in the exact order PruneSupersededGenerations issues them.
//
// Adding a delete to that function without adding its result here shifts every
// later result by one and silently drops the last statement off the end of the
// script, where generationRetentionFakeTx.ExecContext falls back to
// fakeResult{} — whose RowsAffected() is 1, not an error. #5984 hit exactly
// that: the trailing scope-generations delete read 1 instead of 100. The
// callers' script-length assertion is what turns the next such shift into a
// named failure instead of a mystery count.
func generationRetentionExecResults(
	eventCount int,
	sharedProjectionIntents int64,
	sharedProjectionUnroutableIntents int64,
	contentReferences int64,
	contentEntities int64,
	contentFiles int64,
	scopeGenerations int64,
) []sql.Result {
	results := make([]sql.Result, 0, eventCount+6)
	for i := 0; i < eventCount; i++ {
		results = append(results, fakeRowsAffected{})
	}
	results = append(
		results,
		fakeRowsAffected{n: sharedProjectionIntents},
		fakeRowsAffected{n: sharedProjectionUnroutableIntents},
		fakeRowsAffected{n: contentReferences},
		fakeRowsAffected{n: contentEntities},
		fakeRowsAffected{n: contentFiles},
		fakeRowsAffected{n: scopeGenerations},
	)
	return results
}
