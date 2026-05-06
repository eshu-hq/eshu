package telemetry

import "sync/atomic"

var skippedRefreshCount atomic.Uint64

// RecordSkippedRefresh increments the process-local skipped refresh counter.
func RecordSkippedRefresh() uint64 {
	return skippedRefreshCount.Add(1)
}

// SkippedRefreshCount reports the process-local skipped refresh counter.
func SkippedRefreshCount() uint64 {
	return skippedRefreshCount.Load()
}

// ResetSkippedRefreshCountForTesting clears the skipped refresh counter.
func ResetSkippedRefreshCountForTesting() {
	skippedRefreshCount.Store(0)
}
