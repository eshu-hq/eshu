package status

import (
	"fmt"
	"strings"
)

func evaluateHealth(
	queue QueueSnapshot,
	generationTotals map[string]int,
	domainBacklogs []DomainBacklog,
	coordinator *CoordinatorSnapshot,
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
	if queue.Outstanding > 0 && queue.InFlight == 0 && queue.OldestOutstandingAge >= opts.StallAfter {
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
	if coordinatorStalled := coordinatorStalledReason(coordinator, opts); coordinatorStalled != "" {
		return HealthSummary{
			State:   healthStalled,
			Reasons: []string{coordinatorStalled},
		}
	}
	if queue.DeadLetter > 0 || queue.Failed > 0 || generationTotals["failed"] > 0 || coordinatorDegraded(coordinator) {
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

func coordinatorDegraded(snapshot *CoordinatorSnapshot) bool {
	return len(coordinatorDegradedReasons(snapshot)) > 0
}

func coordinatorDegradedReasons(snapshot *CoordinatorSnapshot) []string {
	if snapshot == nil {
		return nil
	}
	runCounts := toCountMap(snapshot.RunStatusCounts)
	workItemCounts := toCountMap(snapshot.WorkItemStatusCounts)
	completenessCounts := toCountMap(snapshot.CompletenessCounts)

	reasons := make([]string, 0, 3)
	if runCounts["failed"] > 0 {
		reasons = append(reasons, fmt.Sprintf("workflow coordinator failed runs=%d", runCounts["failed"]))
	}
	if blocked := completenessCounts["blocked"]; blocked > 0 {
		reasons = append(reasons, fmt.Sprintf("workflow coordinator blocked completeness=%d", blocked))
	}
	if terminal := workItemCounts["failed_terminal"] + workItemCounts["expired"]; terminal > 0 {
		reasons = append(reasons, fmt.Sprintf("workflow coordinator terminal work items=%d", terminal))
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
