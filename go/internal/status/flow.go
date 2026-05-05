package status

import (
	"fmt"
	"strings"
)

// FlowSummary describes one operator-facing lane in the collector/projector/
// reducer flow.
type FlowSummary struct {
	Lane     string
	Source   string
	Progress string
	Backlog  string
}

func buildFlowSummaries(
	scopeTotals map[string]int,
	generationTotals map[string]int,
	stageSummaries []StageSummary,
	queue QueueSnapshot,
	domainBacklogs []DomainBacklog,
) []FlowSummary {
	return []FlowSummary{
		{
			Lane:     "collector",
			Source:   "live",
			Progress: fmt.Sprintf("scopes %s", formatNamedTotals(scopeTotals)),
			Backlog:  fmt.Sprintf("generations %s", formatNamedTotals(generationTotals)),
		},
		{
			Lane:     "projector",
			Source:   "live",
			Progress: fmt.Sprintf("stage %s", stageSummaryText(stageSummaries, "projector")),
			Backlog:  fmt.Sprintf("queue %s", queuePressureText(queue)),
		},
		{
			Lane:     "reducer",
			Source:   "live",
			Progress: fmt.Sprintf("stage %s", stageSummaryText(stageSummaries, "reducer")),
			Backlog:  fmt.Sprintf("top domain %s", domainBacklogText(domainBacklogs)),
		},
	}
}

func renderFlowLines(rows []FlowSummary) []string {
	if len(rows) == 0 {
		return nil
	}

	lines := []string{"Flow:"}
	for _, row := range rows {
		lines = append(
			lines,
			fmt.Sprintf(
				"  %s source=%s progress=%s backlog=%s",
				row.Lane,
				row.Source,
				row.Progress,
				row.Backlog,
			),
		)
	}

	return lines
}

func flowSummariesJSON(rows []FlowSummary) []flowSummaryJSON {
	if len(rows) == 0 {
		return nil
	}

	payload := make([]flowSummaryJSON, 0, len(rows))
	for _, row := range rows {
		payload = append(payload, flowSummaryJSON(row))
	}

	return payload
}

type flowSummaryJSON struct {
	Lane     string `json:"lane"`
	Source   string `json:"source"`
	Progress string `json:"progress"`
	Backlog  string `json:"backlog"`
}

func stageSummaryText(rows []StageSummary, stage string) string {
	for _, row := range rows {
		if row.Stage != stage {
			continue
		}
		return fmt.Sprintf(
			"pending=%d claimed=%d running=%d retrying=%d succeeded=%d dead_letter=%d failed=%d",
			row.Pending,
			row.Claimed,
			row.Running,
			row.Retrying,
			row.Succeeded,
			row.DeadLetter,
			row.Failed,
		)
	}

	return "none"
}

func queuePressureText(queue QueueSnapshot) string {
	return fmt.Sprintf(
		"outstanding=%d in_flight=%d retrying=%d dead_letter=%d failed=%d oldest=%s overdue_claims=%d",
		queue.Outstanding,
		queue.InFlight,
		queue.Retrying,
		queue.DeadLetter,
		queue.Failed,
		queue.OldestOutstandingAge,
		queue.OverdueClaims,
	)
}

func domainBacklogText(rows []DomainBacklog) string {
	if len(rows) == 0 {
		return "none"
	}

	row := rows[0]
	parts := []string{
		row.Domain,
		fmt.Sprintf("outstanding=%d", row.Outstanding),
		fmt.Sprintf("retrying=%d", row.Retrying),
		fmt.Sprintf("dead_letter=%d", row.DeadLetter),
		fmt.Sprintf("failed=%d", row.Failed),
		fmt.Sprintf("oldest=%s", row.OldestAge),
	}

	return strings.Join(parts, " ")
}
