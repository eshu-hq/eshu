package main

import "testing"

func TestGoldenSnapshotRequiredCorrelationIDsAreUnique(t *testing.T) {
	t.Parallel()

	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	seen := make(map[string]struct{}, len(snap.Graph.RequiredCorrelations))
	for _, correlation := range snap.Graph.RequiredCorrelations {
		if _, duplicate := seen[correlation.ID]; duplicate {
			t.Errorf("required_correlations contains duplicate id %q", correlation.ID)
		}
		seen[correlation.ID] = struct{}{}
	}
}
