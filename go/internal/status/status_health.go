package status

import (
	"fmt"
	"strings"
)

func evaluateHealth(
	queue QueueSnapshot,
	generationTotals map[string]int,
	domainBacklogs []DomainBacklog,
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
	if queue.DeadLetter > 0 || queue.Failed > 0 || generationTotals["failed"] > 0 {
		reasons := make([]string, 0, 3)
		if queue.DeadLetter > 0 {
			reasons = append(reasons, fmt.Sprintf("%d work items are dead-lettered", queue.DeadLetter))
		}
		if queue.Failed > 0 {
			reasons = append(reasons, fmt.Sprintf("%d legacy work items remain failed", queue.Failed))
		}
		if generationTotals["failed"] > 0 {
			reasons = append(reasons, fmt.Sprintf("%d generations are failed", generationTotals["failed"]))
		}
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
