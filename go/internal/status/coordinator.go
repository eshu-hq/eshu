// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/shared"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// CoordinatorSnapshot captures additive workflow-coordinator state without
// redefining the platform health contract.
type CoordinatorSnapshot struct {
	CollectorInstances    []collector.InstanceSummary      `json:"collector_instances"`
	RunStatusCounts       []NamedCount                     `json:"run_status_counts"`
	WorkItemStatusCounts  []NamedCount                     `json:"work_item_status_counts"`
	CompletenessCounts    []NamedCount                     `json:"completeness_counts"`
	CollectorBackpressure []collector.BackpressureSnapshot `json:"collector_backpressure"`
	ActiveClaims          int                              `json:"active_claims"`
	OverdueClaims         int                              `json:"overdue_claims"`
	OldestPendingAge      time.Duration                    `json:"oldest_pending_age"`
	// RecentFailures carries failure counts bounded to a recent time window so
	// the degraded health state reflects active failures, not aged all-time
	// totals. A nil value means the reader did not compute a window; callers
	// then fall back to the cumulative counts above to avoid masking failures.
	RecentFailures *CoordinatorRecentFailures `json:"recent_failures,omitempty"`
}

// CoordinatorRecentFailures captures workflow-coordinator failure counts that
// occurred within a bounded recent window (by row updated_at). It drives the
// degraded health state so a recovered stack reports healthy again instead of
// staying degraded until aged failure rows are pruned.
type CoordinatorRecentFailures struct {
	// Window is the lookback used to compute the recent counts.
	Window time.Duration `json:"window"`
	// FailedRuns counts workflow runs whose failed status was last updated
	// within the window.
	FailedRuns int `json:"failed_runs"`
	// BlockedCompleteness counts run-completeness rows blocked within the
	// window.
	BlockedCompleteness int `json:"blocked_completeness"`
	// TerminalWorkItems counts work items that became failed_terminal or
	// expired within the window.
	TerminalWorkItems int `json:"terminal_work_items"`
}

// Active reports whether any failure was observed within the recent window.
func (r *CoordinatorRecentFailures) Active() bool {
	if r == nil {
		return false
	}
	return r.FailedRuns > 0 || r.BlockedCompleteness > 0 || r.TerminalWorkItems > 0
}

func cloneCoordinatorSnapshot(snapshot *CoordinatorSnapshot) *CoordinatorSnapshot {
	if snapshot == nil {
		return nil
	}
	cloned := &CoordinatorSnapshot{
		CollectorInstances:    slices.Clone(snapshot.CollectorInstances),
		RunStatusCounts:       slices.Clone(snapshot.RunStatusCounts),
		WorkItemStatusCounts:  slices.Clone(snapshot.WorkItemStatusCounts),
		CompletenessCounts:    slices.Clone(snapshot.CompletenessCounts),
		CollectorBackpressure: collector.CloneBackpressure(snapshot.CollectorBackpressure),
		ActiveClaims:          snapshot.ActiveClaims,
		OverdueClaims:         snapshot.OverdueClaims,
		OldestPendingAge:      shared.NonNegativeDuration(snapshot.OldestPendingAge),
		RecentFailures:        cloneCoordinatorRecentFailures(snapshot.RecentFailures),
	}
	return cloned
}

func cloneCoordinatorRecentFailures(recent *CoordinatorRecentFailures) *CoordinatorRecentFailures {
	if recent == nil {
		return nil
	}
	cloned := *recent
	cloned.Window = shared.NonNegativeDuration(recent.Window)
	return &cloned
}

func renderCoordinatorLines(snapshot *CoordinatorSnapshot) []string {
	if snapshot == nil {
		return nil
	}

	lines := []string{
		fmt.Sprintf(
			"Coordinator: instances=%d active_claims=%d overdue_claims=%d oldest_pending=%s",
			len(snapshot.CollectorInstances),
			snapshot.ActiveClaims,
			snapshot.OverdueClaims,
			snapshot.OldestPendingAge,
		),
	}
	if len(snapshot.RunStatusCounts) > 0 {
		lines = append(lines, fmt.Sprintf("Coordinator runs: %s", shared.FormatTotals(shared.CountMap(snapshot.RunStatusCounts))))
	}
	if len(snapshot.WorkItemStatusCounts) > 0 {
		lines = append(lines, fmt.Sprintf("Coordinator work items: %s", shared.FormatTotals(shared.CountMap(snapshot.WorkItemStatusCounts))))
	}
	if len(snapshot.CompletenessCounts) > 0 {
		lines = append(lines, fmt.Sprintf("Coordinator completeness: %s", shared.FormatTotals(shared.CountMap(snapshot.CompletenessCounts))))
	}
	lines = append(lines, collector.RenderBackpressureLines(snapshot.CollectorBackpressure)...)
	if len(snapshot.CollectorInstances) > 0 {
		lines = append(lines, "Collector instances:")
		for _, instance := range snapshot.CollectorInstances {
			line := fmt.Sprintf(
				"  %s kind=%s mode=%s enabled=%t bootstrap=%t claims_enabled=%t",
				instance.InstanceID,
				instance.CollectorKind,
				instance.Mode,
				instance.Enabled,
				instance.Bootstrap,
				instance.ClaimsEnabled,
			)
			if strings.TrimSpace(instance.DisplayName) != "" {
				line += fmt.Sprintf(" display_name=%s", instance.DisplayName)
			}
			lines = append(lines, line)
		}
	}
	return lines
}
