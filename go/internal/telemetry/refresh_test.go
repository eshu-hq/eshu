package telemetry

import "testing"

func TestSkippedRefreshCounterTracksRecords(t *testing.T) {
	ResetSkippedRefreshCountForTesting()
	t.Cleanup(ResetSkippedRefreshCountForTesting)

	if got := SkippedRefreshCount(); got != 0 {
		t.Fatalf("SkippedRefreshCount() = %d, want 0", got)
	}

	if got := RecordSkippedRefresh(); got != 1 {
		t.Fatalf("RecordSkippedRefresh() = %d, want 1", got)
	}
	if got := RecordSkippedRefresh(); got != 2 {
		t.Fatalf("RecordSkippedRefresh() = %d, want 2", got)
	}
	if got := SkippedRefreshCount(); got != 2 {
		t.Fatalf("SkippedRefreshCount() = %d, want 2", got)
	}
}
