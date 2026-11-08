// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"fmt"
	"testing"
)

// Committable 5.2 coverage for eshu-hq/eshu#10354 (Phase 5 5.2), carried
// from the deleted 5.1 scratch shims
// (docs/internal/evidence/9961-shard-scoped-generations.md). A heavy
// real-world CVE List delta day touches ~2,000 CVEs; dirty-shard fan-in,
// not per-shard cost, bounds the daily work.
func TestCVEListDeltaCountsHeavyDay(t *testing.T) {
	t.Parallel()

	// 2,000 modified CVEs concentrated in two shards replay to 2 dirty
	// shards (2x the observed heavy-day shape from 5.1).
	concentrated := make([]string, 0, 2000)
	for i := 0; i < 1000; i++ {
		concentrated = append(concentrated, fmt.Sprintf("CVE-2024-%04d", 1000+i))
	}
	for i := 0; i < 1000; i++ {
		concentrated = append(concentrated, fmt.Sprintf("CVE-2024-%04d", 2000+i))
	}
	shards, skipped := CVEListDirtyShards(concentrated)
	if skipped != 0 {
		t.Fatalf("CVEListDirtyShards() skipped = %d, want 0 (all IDs valid)", skipped)
	}
	if len(shards) != 2 {
		t.Fatalf("CVEListDirtyShards() = %v, want 2 dirty shards", shards)
	}

	// The same 2,000 CVEs spread across six years replay to 12 dirty shards.
	spread := make([]string, 0, 2000)
	years := []string{"2019", "2020", "2021", "2022", "2023", "2024"}
	// Sequences 800..1132 cross the x1000 group boundary, so each year
	// contributes two shards (0xxx and 1xxx): 12 total.
	for i := 0; i < 2000; i++ {
		spread = append(spread, fmt.Sprintf("CVE-%s-%04d", years[i%len(years)], 800+(i/len(years))))
	}
	shards, skipped = CVEListDirtyShards(spread)
	if skipped != 0 {
		t.Fatalf("CVEListDirtyShards() skipped = %d, want 0 (all IDs valid)", skipped)
	}
	if len(shards) != 12 {
		t.Fatalf("CVEListDirtyShards() = %v, want 12 dirty shards", shards)
	}

	// Every dirty shard gets exactly one full-replace generation.
	gens := CVEListShardGenerations(shards, nil)
	if len(gens) != len(shards) {
		t.Fatalf("CVEListShardGenerations() = %d generations, want %d", len(gens), len(shards))
	}
}

// BenchmarkCVEListFullDayDeltaReplay measures the 5.1 T3 path in committed
// form: 2,000-CVE delta replay (dirty-shard derivation plus one generation
// per dirty shard). 5.1 scratch measured ~0.16 ms wall-clock (T3 budget:
// <10 min on dev-class hardware).
func BenchmarkCVEListFullDayDeltaReplay(b *testing.B) {
	spread := make([]string, 0, 2000)
	years := []string{"2019", "2020", "2021", "2022", "2023", "2024"}
	for i := 0; i < 2000; i++ {
		spread = append(spread, fmt.Sprintf("CVE-%s-%04d", years[i%len(years)], 800+(i/len(years))))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		shards, skipped := CVEListDirtyShards(spread)
		if skipped != 0 {
			b.Fatalf("CVEListDirtyShards() skipped = %d, want 0", skipped)
		}
		_ = CVEListShardGenerations(shards, nil)
	}
}
