// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"
	"testing"
)

func TestSkippedRefreshCounterTracksRecords(t *testing.T) {
	ResetSkippedRefreshCountForTesting()
	t.Cleanup(ResetSkippedRefreshCountForTesting)

	if got := SkippedRefreshCount(); got != 0 {
		t.Fatalf("SkippedRefreshCount() = %d, want 0", got)
	}

	if got := RecordSkippedRefresh(RefreshSkipReasonUnchanged); got != 1 {
		t.Fatalf("RecordSkippedRefresh() = %d, want 1", got)
	}
	if got := RecordSkippedRefresh(RefreshSkipReasonReconcileReprojected); got != 2 {
		t.Fatalf("RecordSkippedRefresh() = %d, want 2", got)
	}
	if got := SkippedRefreshCount(); got != 2 {
		t.Fatalf("SkippedRefreshCount() = %d, want 2", got)
	}
}

// TestSkippedRefreshCountByReasonIsClosed proves #7978's reason label: every
// record lands on one value of the closed set, and an unknown reason lands on
// RefreshSkipReasonOther instead of minting a new label value.
func TestSkippedRefreshCountByReasonIsClosed(t *testing.T) {
	ResetSkippedRefreshCountForTesting()
	t.Cleanup(ResetSkippedRefreshCountForTesting)

	RecordSkippedRefresh(RefreshSkipReasonUnchanged)
	RecordSkippedRefresh(RefreshSkipReasonPendingSameIdentity)
	RecordSkippedRefresh(RefreshSkipReasonReconcileReprojected)
	RecordSkippedRefresh(RefreshSkipReasonReconcileReprojected)
	RecordSkippedRefresh(RefreshSkipReasonReconcileReprojectionPending)
	RecordSkippedRefresh(RefreshSkipReasonFinalizedGenerationReplay)
	RecordSkippedRefresh(RefreshSkipReasonReconcileSuperseded)
	RecordSkippedRefresh(RefreshSkipReasonDestructiveSuccessor)
	RecordSkippedRefresh("repo-specific-free-text")

	want := map[string]uint64{
		RefreshSkipReasonUnchanged:                    1,
		RefreshSkipReasonPendingSameIdentity:          1,
		RefreshSkipReasonReconcileReprojected:         2,
		RefreshSkipReasonReconcileReprojectionPending: 1,
		RefreshSkipReasonFinalizedGenerationReplay:    1,
		RefreshSkipReasonReconcileSuperseded:          1,
		RefreshSkipReasonDestructiveSuccessor:         1,
		RefreshSkipReasonOther:                        1,
	}
	reasons := RefreshSkipReasons()
	if len(reasons) != len(want) {
		t.Fatalf("RefreshSkipReasons() = %v, want %d values", reasons, len(want))
	}
	for reason, count := range want {
		if !slices.Contains(reasons, reason) {
			t.Fatalf("RefreshSkipReasons() = %v, missing %q", reasons, reason)
		}
		if got := SkippedRefreshCountByReason(reason); got != count {
			t.Fatalf("SkippedRefreshCountByReason(%q) = %d, want %d", reason, got, count)
		}
	}
	if got := SkippedRefreshCountByReason("repo-specific-free-text"); got != 0 {
		t.Fatalf("SkippedRefreshCountByReason(unknown) = %d, want 0", got)
	}
	if got := SkippedRefreshCount(); got != 9 {
		t.Fatalf("SkippedRefreshCount() = %d, want 9", got)
	}
	if !slices.Contains(LogKeys(), LogKeyRefreshSkipReason) {
		t.Fatalf("LogKeys() missing %q", LogKeyRefreshSkipReason)
	}
}
