// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"
	"sync/atomic"
)

// Refresh skip reasons label eshu_runtime_refresh_skipped_by_reason_total and
// the refresh_skip_reason log key (#7978). The set is closed: an unknown
// reason records as RefreshSkipReasonOther, so a caller cannot add a label
// value by accident.
const (
	// RefreshSkipReasonUnchanged means the snapshot's freshness hint and
	// commit equal the scope's active generation, so the ingestion store
	// stored nothing.
	RefreshSkipReasonUnchanged = "unchanged"
	// RefreshSkipReasonPendingSameIdentity means the snapshot's hint and
	// commit equal a generation that is still pending (ruling a9-120): the
	// projector has not activated it yet, so the store stored nothing. A
	// rising count means the projector or reducer is behind.
	RefreshSkipReasonPendingSameIdentity = "pending_same_identity"
	// RefreshSkipReasonReconcileReprojected means a reconciliation snapshot
	// matched the active generation, so the store re-opened that generation's
	// projector work instead of storing a duplicate generation.
	RefreshSkipReasonReconcileReprojected = "reconcile_reprojected"
	// RefreshSkipReasonReconcileReprojectionPending means a reconciliation
	// snapshot matched the active generation whose projector work was already
	// pending, retrying, or under a live lease, so the store stored nothing and
	// stamped nothing: the work already queued is the re-projection.
	RefreshSkipReasonReconcileReprojectionPending = "reconcile_reprojection_pending"
	// RefreshSkipReasonReconcileSuperseded means a reconciliation snapshot
	// matched the active generation, but a newer generation activated before
	// the re-open took the scope lock. The snapshot describes an older commit,
	// so it is dropped; the sweep decides the scope again next cycle.
	RefreshSkipReasonReconcileSuperseded = "reconcile_superseded"
	// RefreshSkipReasonFinalizedGenerationReplay means a retry tried to
	// re-commit a generation that is already finalized.
	RefreshSkipReasonFinalizedGenerationReplay = "finalized_generation_replay"
	// RefreshSkipReasonOther counts a record with a reason outside this set.
	RefreshSkipReasonOther = "other"
)

// refreshSkipReasons is the closed, ordered reason set. The counter array
// below is sized from it, so adding a reason here cannot index past the end.
var refreshSkipReasons = [...]string{
	RefreshSkipReasonUnchanged,
	RefreshSkipReasonPendingSameIdentity,
	RefreshSkipReasonReconcileReprojected,
	RefreshSkipReasonReconcileReprojectionPending,
	RefreshSkipReasonReconcileSuperseded,
	RefreshSkipReasonFinalizedGenerationReplay,
	RefreshSkipReasonOther,
}

var (
	skippedRefreshCount         atomic.Uint64
	skippedRefreshCountByReason [len(refreshSkipReasons)]atomic.Uint64
)

// refreshSkipReasonIndex maps reason onto the closed set.
func refreshSkipReasonIndex(reason string) int {
	if index := slices.Index(refreshSkipReasons[:], reason); index >= 0 {
		return index
	}
	return len(refreshSkipReasons) - 1
}

// RefreshSkipReasons returns the closed, ordered set of refresh skip reasons.
func RefreshSkipReasons() []string {
	return slices.Clone(refreshSkipReasons[:])
}

// RecordSkippedRefresh increments the process-local skipped refresh counter
// and the counter for reason, which is mapped onto the closed reason set. It
// returns the new total.
func RecordSkippedRefresh(reason string) uint64 {
	skippedRefreshCountByReason[refreshSkipReasonIndex(reason)].Add(1)
	return skippedRefreshCount.Add(1)
}

// SkippedRefreshCount reports the process-local skipped refresh total.
func SkippedRefreshCount() uint64 {
	return skippedRefreshCount.Load()
}

// SkippedRefreshCountByReason reports the process-local skipped refresh count
// for one reason of the closed set, or zero for any other value.
func SkippedRefreshCountByReason(reason string) uint64 {
	index := slices.Index(refreshSkipReasons[:], reason)
	if index < 0 {
		return 0
	}
	return skippedRefreshCountByReason[index].Load()
}

// ResetSkippedRefreshCountForTesting clears the skipped refresh counters.
func ResetSkippedRefreshCountForTesting() {
	skippedRefreshCount.Store(0)
	for index := range skippedRefreshCountByReason {
		skippedRefreshCountByReason[index].Store(0)
	}
}
