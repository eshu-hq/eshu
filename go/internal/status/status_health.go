// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"fmt"
	"strings"
)

func evaluateHealth(
	queue QueueSnapshot,
	generationTotals map[string]int,
	domainBacklogs []DomainBacklog,
	producerActivity ProducerActivitySnapshot,
	coordinator *CoordinatorSnapshot,
	collectorGenerationDeadLetters CollectorGenerationDeadLetterSnapshot,
	opts Options,
) HealthSummary {
	if queue.OverdueClaims > 0 {
		return HealthSummary{
			State: healthStalled,
			Reasons: []string{
				fmt.Sprintf("%d overdue claims suggest stuck workers", queue.OverdueClaims),
			},
		}
	}
	if coordinator != nil && coordinator.OverdueClaims > 0 {
		return HealthSummary{
			State: healthStalled,
			Reasons: []string{
				fmt.Sprintf("workflow coordinator has %d overdue claims", coordinator.OverdueClaims),
			},
		}
	}
	producerActive := recentProducerActivityReason(producerActivity, opts)
	if queue.Outstanding > 0 && queue.InFlight == 0 && queue.OldestOutstandingAge >= opts.StallAfter {
		if producerActive == "" {
			if backlog := largestDomainBacklog(domainBacklogs); backlog.Outstanding > 0 {
				oldestAge := backlog.OldestAge
				if oldestAge <= 0 {
					oldestAge = queue.OldestOutstandingAge
				}
				return HealthSummary{
					State: healthStalled,
					Reasons: []string{
						fmt.Sprintf(
							"domain %s has %d outstanding items with no in-flight work for %s",
							backlog.Domain,
							backlog.Outstanding,
							oldestAge,
						),
					},
				}
			}
			return HealthSummary{
				State: healthStalled,
				Reasons: []string{
					fmt.Sprintf(
						"backlog has %d outstanding items with no in-flight work for %s",
						queue.Outstanding,
						queue.OldestOutstandingAge,
					),
				},
			}
		}
	}
	if coordinatorStalled := coordinatorStalledReason(coordinator, opts); coordinatorStalled != "" {
		return HealthSummary{
			State:   healthStalled,
			Reasons: []string{coordinatorStalled},
		}
	}
	collectorGenerationDeadLetters = cloneCollectorGenerationDeadLetterSnapshot(collectorGenerationDeadLetters)
	unresolvedCollectorGenerations := collectorGenerationDeadLetters.DeadLetter +
		collectorGenerationDeadLetters.ReplayRequested
	if queue.DeadLetter > 0 || queue.Failed > 0 || generationTotals["failed"] > 0 || coordinatorDegraded(coordinator) ||
		unresolvedCollectorGenerations > 0 {
		reasons := make([]string, 0, 6)
		if queue.DeadLetter > 0 {
			reasons = append(reasons, fmt.Sprintf("%d work items are dead-lettered", queue.DeadLetter))
		}
		if queue.Failed > 0 {
			reasons = append(reasons, fmt.Sprintf("%d legacy work items remain failed", queue.Failed))
		}
		if generationTotals["failed"] > 0 {
			reasons = append(reasons, fmt.Sprintf("%d generations are failed", generationTotals["failed"]))
		}
		if collectorGenerationDeadLetters.DeadLetter > 0 {
			reasons = append(
				reasons,
				fmt.Sprintf("%d collector generations are dead-lettered", collectorGenerationDeadLetters.DeadLetter),
			)
		}
		if collectorGenerationDeadLetters.ReplayRequested > 0 {
			if collectorGenerationDeadLetters.ReplayRequested == 1 {
				reasons = append(reasons, "1 collector generation replay request is unresolved")
			} else {
				reasons = append(
					reasons,
					fmt.Sprintf(
						"%d collector generation replay requests are unresolved",
						collectorGenerationDeadLetters.ReplayRequested,
					),
				)
			}
		}
		reasons = append(reasons, coordinatorDegradedReasons(coordinator)...)
		return HealthSummary{
			State:   healthDegraded,
			Reasons: reasons,
		}
	}
	if queue.Outstanding > 0 || generationTotals["pending"] > 0 {
		reason := "work remains queued"
		if queue.InFlight > 0 {
			reason = fmt.Sprintf("%d work items are currently in flight", queue.InFlight)
		} else if producerActive != "" {
			reason = producerActive
		}
		return HealthSummary{
			State:   healthProgressing,
			Reasons: []string{reason},
		}
	}
	if sharedBacklog := sharedProjectionBacklog(domainBacklogs); sharedBacklog.Outstanding > 0 {
		if sharedBacklog.InFlight > 0 {
			return HealthSummary{
				State: healthProgressing,
				Reasons: []string{
					fmt.Sprintf(
						"shared projection domain %s has %d outstanding intents with %d in flight",
						sharedBacklog.Domain,
						sharedBacklog.Outstanding,
						sharedBacklog.InFlight,
					),
				},
			}
		}
		if sharedBacklog.OldestAge >= opts.StallAfter {
			return HealthSummary{
				State: healthStalled,
				Reasons: []string{
					fmt.Sprintf(
						"shared projection domain %s has %d outstanding intents for %s",
						sharedBacklog.Domain,
						sharedBacklog.Outstanding,
						sharedBacklog.OldestAge,
					),
				},
			}
		}

		return HealthSummary{
			State: healthProgressing,
			Reasons: []string{
				fmt.Sprintf(
					"shared projection domain %s has %d outstanding intents",
					sharedBacklog.Domain,
					sharedBacklog.Outstanding,
				),
			},
		}
	}
	if coordinatorProgress := coordinatorProgressReason(coordinator); coordinatorProgress != "" {
		return HealthSummary{
			State:   healthProgressing,
			Reasons: []string{coordinatorProgress},
		}
	}

	return HealthSummary{
		State:   healthHealthy,
		Reasons: []string{"no outstanding queue backlog"},
	}
}

func recentProducerActivityReason(snapshot ProducerActivitySnapshot, opts Options) string {
	if !snapshot.HasActiveOrPendingGeneration || snapshot.LatestGenerationAge >= opts.StallAfter {
		return ""
	}
	return fmt.Sprintf("recent producer activity observed %s ago", snapshot.LatestGenerationAge)
}

// sharedProjectionBacklog returns the largest outstanding domain backlog after
// the fact queue is drained. Lease-only rows remain visible in domain_backlogs,
// but without outstanding intents they are worker activity rather than
// unfinished graph-visible work.
func sharedProjectionBacklog(rows []DomainBacklog) DomainBacklog {
	var largest DomainBacklog
	for _, row := range rows {
		if strings.TrimSpace(row.Domain) == "" || row.Outstanding <= 0 {
			continue
		}
		if row.Outstanding > largest.Outstanding ||
			(row.Outstanding == largest.Outstanding && row.InFlight > largest.InFlight) ||
			(row.Outstanding == largest.Outstanding && row.InFlight == largest.InFlight && row.OldestAge > largest.OldestAge) {
			largest = row
		}
	}

	return largest
}

func largestDomainBacklog(rows []DomainBacklog) DomainBacklog {
	var largest DomainBacklog
	for _, row := range rows {
		if strings.TrimSpace(row.Domain) == "" || row.Outstanding <= 0 || row.InFlight > 0 {
			continue
		}
		if row.Outstanding > largest.Outstanding ||
			(row.Outstanding == largest.Outstanding && row.OldestAge > largest.OldestAge) {
			largest = row
		}
	}
	return largest
}

func coordinatorDegraded(snapshot *CoordinatorSnapshot) bool {
	return len(coordinatorDegradedReasons(snapshot)) > 0
}

// coordinatorDegradedReasons returns the operator-facing reasons that put the
// workflow coordinator into a degraded state.
//
// When the snapshot carries a recent-failure window, the degraded state is
// driven only by failures observed within that window so a recovered stack no
// longer reports degraded forever on aged all-time failures. Cumulative totals
// are still appended as informational detail so no data is lost. When the
// window is absent (nil), the legacy cumulative behavior is preserved so
// readers that do not compute a window never silently mask active failures.
func coordinatorDegradedReasons(snapshot *CoordinatorSnapshot) []string {
	if snapshot == nil {
		return nil
	}
	runCounts := toCountMap(snapshot.RunStatusCounts)
	workItemCounts := toCountMap(snapshot.WorkItemStatusCounts)
	completenessCounts := toCountMap(snapshot.CompletenessCounts)
	cumulativeFailedRuns := runCounts["failed"]
	cumulativeBlocked := completenessCounts["blocked"]
	cumulativeTerminal := workItemCounts["failed_terminal"] + workItemCounts["expired"]

	if recent := snapshot.RecentFailures; recent != nil {
		return recentCoordinatorDegradedReasons(recent, cumulativeFailedRuns, cumulativeBlocked, cumulativeTerminal)
	}

	reasons := make([]string, 0, 3)
	if cumulativeFailedRuns > 0 {
		reasons = append(reasons, fmt.Sprintf("workflow coordinator failed runs=%d", cumulativeFailedRuns))
	}
	if cumulativeBlocked > 0 {
		reasons = append(reasons, fmt.Sprintf("workflow coordinator blocked completeness=%d", cumulativeBlocked))
	}
	if cumulativeTerminal > 0 {
		reasons = append(reasons, fmt.Sprintf("workflow coordinator terminal work items=%d", cumulativeTerminal))
	}
	return reasons
}

// recentCoordinatorDegradedReasons builds degraded reasons from windowed
// failure counts. State is driven by recent counts; the matching cumulative
// total is appended in parentheses so operators keep the all-time context.
func recentCoordinatorDegradedReasons(
	recent *CoordinatorRecentFailures,
	cumulativeFailedRuns int,
	cumulativeBlocked int,
	cumulativeTerminal int,
) []string {
	if !recent.Active() {
		return nil
	}
	window := nonNegativeDuration(recent.Window)
	reasons := make([]string, 0, 3)
	if recent.FailedRuns > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"workflow coordinator recent failed runs=%d in %s (cumulative=%d)",
			recent.FailedRuns, window, cumulativeFailedRuns,
		))
	}
	if recent.BlockedCompleteness > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"workflow coordinator recent blocked completeness=%d in %s (cumulative=%d)",
			recent.BlockedCompleteness, window, cumulativeBlocked,
		))
	}
	if recent.TerminalWorkItems > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"workflow coordinator recent terminal work items=%d in %s (cumulative=%d)",
			recent.TerminalWorkItems, window, cumulativeTerminal,
		))
	}
	return reasons
}

func coordinatorStalledReason(snapshot *CoordinatorSnapshot, opts Options) string {
	if snapshot == nil || snapshot.OldestPendingAge < opts.StallAfter {
		return ""
	}
	workItemCounts := toCountMap(snapshot.WorkItemStatusCounts)
	if workItemCounts["pending"] == 0 || snapshot.ActiveClaims > 0 {
		return ""
	}
	return fmt.Sprintf(
		"workflow coordinator has %d pending work items for %s with no active claims",
		workItemCounts["pending"],
		snapshot.OldestPendingAge,
	)
}

func coordinatorProgressReason(snapshot *CoordinatorSnapshot) string {
	if snapshot == nil {
		return ""
	}
	runCounts := toCountMap(snapshot.RunStatusCounts)
	workItemCounts := toCountMap(snapshot.WorkItemStatusCounts)
	completenessCounts := toCountMap(snapshot.CompletenessCounts)

	parts := make([]string, 0, 8)
	for _, status := range []string{
		"collection_pending",
		"collection_active",
		"collection_complete",
		"reducer_converging",
	} {
		if count := runCounts[status]; count > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", status, count))
		}
	}
	for _, status := range []string{"pending", "claimed", "failed_retryable"} {
		if count := workItemCounts[status]; count > 0 {
			parts = append(parts, fmt.Sprintf("%s work items=%d", status, count))
		}
	}
	if pending := completenessCounts["pending"]; pending > 0 {
		parts = append(parts, fmt.Sprintf("pending completeness=%d", pending))
	}
	if snapshot.ActiveClaims > 0 {
		parts = append(parts, fmt.Sprintf("active claims=%d", snapshot.ActiveClaims))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("workflow coordinator %s", strings.Join(parts, " "))
}
