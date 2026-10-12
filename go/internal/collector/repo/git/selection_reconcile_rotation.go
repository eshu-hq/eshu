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
