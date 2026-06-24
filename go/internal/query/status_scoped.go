// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func scopedAuthContext(ctx context.Context) bool {
	auth, ok := AuthContextFromContext(ctx)
	return ok && auth.Mode == AuthModeScoped
}

func scopedCoordinatorToMap(snapshot *status.CoordinatorSnapshot) map[string]any {
	if snapshot == nil {
		return map[string]any{}
	}

	result := map[string]any{
		"collector_instance_count": len(snapshot.CollectorInstances),
		"run_status_counts":        namedCountsToSlice(snapshot.RunStatusCounts),
		"work_item_status_counts":  namedCountsToSlice(snapshot.WorkItemStatusCounts),
		"completeness_counts":      namedCountsToSlice(snapshot.CompletenessCounts),
		"active_claims":            snapshot.ActiveClaims,
		"overdue_claims":           snapshot.OverdueClaims,
		"oldest_pending_age":       snapshot.OldestPendingAge.Seconds(),
	}
	if recent := snapshot.RecentFailures; recent != nil {
		result["recent_failures"] = map[string]any{
			"window_seconds":       recent.Window.Seconds(),
			"failed_runs":          recent.FailedRuns,
			"blocked_completeness": recent.BlockedCompleteness,
			"terminal_work_items":  recent.TerminalWorkItems,
		}
	}
	return result
}
