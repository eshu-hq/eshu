// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import "testing"

// TestSnapshotFreshnessHintFoldsFunctionSummaries proves the freshness hint
// changes when FunctionSummaries (carrying the new graph_uid payload) are
// present, so upgrading to the graph_uid-emitting commit does not leave an
// unchanged repo with an identical hint that shouldSkipUnchangedGeneration
// would drain and skip. A gate-off snapshot (no summaries) keeps its existing
// hint byte-for-byte, so no mass re-collection happens when value-flow emission
// is disabled.
func TestSnapshotFreshnessHintFoldsFunctionSummaries(t *testing.T) {
	t.Parallel()

	base := RepositorySnapshot{FileCount: 3}

	baseline := snapshotFreshnessHint(base)

	// No-churn invariant: a nil/empty FunctionSummaries slice must not change
	// the hint relative to the baseline (gate-off snapshots stay identical).
	empty := base
	empty.FunctionSummaries = nil
	if got := snapshotFreshnessHint(empty); got != baseline {
		t.Fatalf("nil FunctionSummaries changed the hint: baseline=%s got=%s", baseline, got)
	}
	emptySlice := base
	emptySlice.FunctionSummaries = []FunctionSummarySnapshot{}
	if got := snapshotFreshnessHint(emptySlice); got != baseline {
		t.Fatalf("empty FunctionSummaries changed the hint: baseline=%s got=%s", baseline, got)
	}

	// Upgrade scenario: the same snapshot with FunctionSummaries present (graph_uid
	// newly appears) must yield a DIFFERENT hint than the gate-off baseline.
	withSummaries := base
	withSummaries.FunctionSummaries = []FunctionSummarySnapshot{
		{FunctionID: "repo-1\x1fpkg\x1f\x1fview", GraphUID: "uid-view"},
		{FunctionID: "repo-1\x1fpkg\x1f\x1fquery", GraphUID: "uid-query"},
	}
	withHint := snapshotFreshnessHint(withSummaries)
	if withHint == baseline {
		t.Fatalf("FunctionSummaries were not folded into the hint: hint=%s equals baseline", withHint)
	}

	// The GraphUID itself must be load-bearing: the same summaries with empty
	// graph_uid (pre-upgrade) must yield a different hint than with graph_uid set.
	withEmptyUID := base
	withEmptyUID.FunctionSummaries = []FunctionSummarySnapshot{
		{FunctionID: "repo-1\x1fpkg\x1f\x1fview", GraphUID: ""},
		{FunctionID: "repo-1\x1fpkg\x1f\x1fquery", GraphUID: ""},
	}
	if got := snapshotFreshnessHint(withEmptyUID); got == withHint {
		t.Fatalf("GraphUID is not folded into the hint: empty-uid hint equals graph_uid hint (%s)", got)
	}

	// Determinism: the same input yields the same hint.
	if again := snapshotFreshnessHint(withSummaries); again != withHint {
		t.Fatalf("hint is not deterministic: first=%s second=%s", withHint, again)
	}
}
