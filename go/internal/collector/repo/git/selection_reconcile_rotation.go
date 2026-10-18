// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// reconcileWalkOrder returns the repository visit order for one selection
// cycle (#7987). The input is the prioritizeRepositoryReindex output: its
// leading per-repository-reindex run stays pinned at the head so an explicit
// reindex still jumps the queue, and the remaining suffix is rotated so the
// entry at start comes first. The order is reproducible for a given
// (repositoryIDs, start) pair. It also returns the prefix length, the
// normalized start, and the suffix length the caller needs to advance the
// offset to the position after the last served suffix entry.
func reconcileWalkOrder(config RepoSyncConfig, ordered []string, baseline gitDeltaBaseline) (walk []string, prefixLen, normStart, suffixLen int) {
	if len(baseline.RepositoryReindexRequestedAt) > 0 {
		for _, repoID := range ordered {
			scopeID := gitScopeIDForRepositoryID(config, repoID)
			if scopeID == "" {
				break
			}
			if _, ok := baseline.RepositoryReindexRequestedAt[scopeID]; !ok {
				break
			}
			prefixLen++
		}
	}
	suffixLen = len(ordered) - prefixLen
	if suffixLen > 0 {
		normStart = baseline.ReconcileStartOffset % suffixLen
		if normStart < 0 {
			normStart += suffixLen
		}
	}
	walk = make([]string, 0, len(ordered))
	walk = append(walk, ordered[:prefixLen]...)
	walk = append(walk, ordered[prefixLen+normStart:]...)
	walk = append(walk, ordered[prefixLen:prefixLen+normStart]...)
	return walk, prefixLen, normStart, suffixLen
}

// reconcileDueUnvisitedReasons is the closed reason set of the
// ReconciliationDueUnvisited gauge: every reason a scope decision can carry
// when the scope is due for a forced full yet took no per-cycle slot.
var reconcileDueUnvisitedReasons = []string{
	reconcileReasonNeverReconciled,
	reconcileReasonIntervalElapsed,
	reconcileReasonInFlightExpired,
	reconcileReasonRetryAfterUnprojected,
	reconcileReasonGraphDirty,
	reconcileReasonReindexRequested,
	reconcileReasonRepositoryReindexRequested,
	scope.ReconcileReasonRequestStale,
}

// isReconcileDueUnvisitedReason reports whether reason belongs to the gauge's
// closed set.
func isReconcileDueUnvisitedReason(reason string) bool {
	for _, known := range reconcileDueUnvisitedReasons {
		if reason == known {
			return true
		}
	}
	return false
}

// reconcileWalkTracker carries the #7987 round-robin state for one selection
// cycle: the rotated walk order plus the offset bookkeeping. The sync walks
// the order, notes each spent slot and each unvisited due repository, then
// finishes the tracker to record the gauge and learn the next cycle's start.
type reconcileWalkTracker struct {
	prefixLen int
	start     int
	suffixLen int
	unvisited map[string]int64
	lastSpent int
	// lastVisited is the suffix position of the last repository the
	// walk reached, spent or not: the resume cursor a yielded sweep
	// reports (#8123).
	lastVisited int
	// walkSuffixLen is how many suffix entries this walk covers: the
	// whole suffix, or the unvisited tail a resumed sweep limits
	// itself to (#8123).
	walkSuffixLen int
}

// trackReconcileWalk returns this cycle's visit order with its tracker: the
// requested prefix stays pinned at the head while the suffix starts at the
// baseline's offset, reproducibly for a given list and offset.
func trackReconcileWalk(config RepoSyncConfig, ordered []string, baseline gitDeltaBaseline) ([]string, *reconcileWalkTracker) {
	walk, prefixLen, normStart, suffixLen := reconcileWalkOrder(config, ordered, baseline)
	return walk, &reconcileWalkTracker{
		prefixLen:     prefixLen,
		start:         normStart,
		suffixLen:     suffixLen,
		unvisited:     make(map[string]int64),
		lastSpent:     -1,
		lastVisited:   -1,
		walkSuffixLen: suffixLen,
	}
}

// noteEvaluated counts a due repository that took no slot toward the
// due-unvisited gauge.
func (t *reconcileWalkTracker) noteEvaluated(due bool, reason string, forced bool) {
	if due && !forced && isReconcileDueUnvisitedReason(reason) {
		t.unvisited[reason]++
	}
}

// noteSpent records a spent per-cycle slot at walk index i, so the next
// cycle resumes past served repositories. Held and failed repositories spend
// nothing and keep their turn.
func (t *reconcileWalkTracker) noteSpent(walkIdx int) {
	if suffixPos := walkIdx - t.prefixLen; suffixPos >= 0 {
		t.lastSpent = suffixPos
	}
}

// noteVisited records the walk reaching index i, so a yielded sweep
// resumes past every repository it already fetched (#8123). A due
// repository that took no slot inside the visited prefix is reached
// again after the walk wraps: its fetch already happened, only the
// forcing defers by at most one rotation, far inside the reconcile
// interval.
func (t *reconcileWalkTracker) noteVisited(walkIdx int) {
	if suffixPos := walkIdx - t.prefixLen; suffixPos >= 0 {
		t.lastVisited = suffixPos
	}
}

// limitWalk truncates the walk to its pinned prefix plus the first
// limit suffix entries: the unvisited tail of a yielded sweep (#8123).
// A non-positive limit keeps the whole walk. The repository list may
// have changed since the yield, so the limit is a count, not an
// identity: an added repository waits for the next logical sweep and a
// removed one just shortens this walk. Both self-heal on the next
// full sweep.
func (t *reconcileWalkTracker) limitWalk(walk []string, limit int) []string {
	if limit <= 0 || limit >= t.suffixLen {
		return walk
	}
	t.walkSuffixLen = limit
	return walk[:t.prefixLen+limit]
}

// finish records the due-unvisited gauge and returns the next cycle's start:
// after the last served suffix position, or the same start when nothing took
// a slot so the same window is retried.
func (t *reconcileWalkTracker) finish(ctx context.Context, baseline gitDeltaBaseline) int {
	nextStart := t.start
	if t.suffixLen > 0 && t.lastSpent >= 0 {
		nextStart = (t.start + t.lastSpent + 1) % t.suffixLen
	}
	baseline.recordReconcileDueUnvisited(ctx, t.unvisited)
	return nextStart
}

// finishYielded ends a walk the sweep cut short for a queued webhook
// trigger (#8123). It records the due-unvisited gauge for the
// repositories this cycle evaluated (the unvisited tail was not
// evaluated, so the gauge resumes its whole-sweep meaning on the
// completing cycle) and returns the next cycle's start — past the
// last visited suffix position — plus how many suffix repositories
// the logical sweep still owes.
func (t *reconcileWalkTracker) finishYielded(ctx context.Context, baseline gitDeltaBaseline) (nextStart, remaining int) {
	nextStart = t.start
	remaining = t.walkSuffixLen
	if t.suffixLen > 0 && t.lastVisited >= 0 {
		nextStart = (t.start + t.lastVisited + 1) % t.suffixLen
		remaining = t.walkSuffixLen - t.lastVisited - 1
	}
	baseline.recordReconcileDueUnvisited(ctx, t.unvisited)
	return nextStart, remaining
}

// recordReconcileDueUnvisited samples, at the end of each selection cycle, how
// many repositories were due for a forced full but took no per-cycle slot, by
// bounded reason (#7987). Every reason in the closed set is recorded every
// cycle, including zero, so a draining backlog shows as a falling gauge rather
// than a stale series. Best-effort: a nil Instruments is a no-op.
func (b gitDeltaBaseline) recordReconcileDueUnvisited(ctx context.Context, unvisited map[string]int64) {
	if b.Instruments == nil || b.Instruments.ReconciliationDueUnvisited == nil {
		return
	}
	for _, reason := range reconcileDueUnvisitedReasons {
		b.Instruments.ReconciliationDueUnvisited.Record(ctx, unvisited[reason], metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionReason, reason),
		))
	}
}
