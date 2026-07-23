// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestGoldenSnapshotRequiresSQLMigrationDropTruth guards #5482: the B-12
// corpus must require the three MIGRATES edges from sql_comprehensive's ALTER
// migration plus its direct comma-separated DROP migration. Without the DROP
// parser path, the count falls to one and both assertions fail.
func TestGoldenSnapshotRequiresSQLMigrationDropTruth(t *testing.T) {
	t.Parallel()

	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	count, ok := snap.Graph.EdgeCounts["MIGRATES"]
	if !ok {
		t.Fatal("edge_counts missing MIGRATES")
	}
	if count.Min != 3 || count.Max != 3 {
		t.Fatalf("edge_counts[MIGRATES] = %+v, want exactly 3", count)
	}

	for _, correlation := range snap.Graph.RequiredCorrelations {
		if correlation.ID != "rc-163" {
			continue
		}
		if correlation.Relationship != "MIGRATES" || correlation.FromLabel != "SqlMigration" || correlation.ToLabel != "SqlTable" {
			t.Fatalf("rc-163 = %+v, want MIGRATES SqlMigration->SqlTable", correlation)
		}
		if correlation.MinimumCount != 3 {
			t.Fatalf("rc-163 minimum_count = %d, want 3", correlation.MinimumCount)
		}
		return
	}
	t.Fatal("required_correlations missing rc-163")
}
