package status

import (
	"fmt"
	"slices"
	"time"
)

// RegistryCollectorSnapshot summarizes claim-driven registry collector runtime
// state without exposing registry hosts, repositories, packages, tags, digests,
// account identifiers, metadata URLs, or credential environment names.
type RegistryCollectorSnapshot struct {
	CollectorKind        string
	ConfiguredInstances  int
	ActiveScopes         int
	CompletedGenerations int
	LastCompletedAt      time.Time
	RetryableFailures    int
	TerminalFailures     int
	FailureClassCounts   []NamedCount
}

func cloneRegistryCollectorSnapshots(rows []RegistryCollectorSnapshot) []RegistryCollectorSnapshot {
	cloned := slices.Clone(rows)
	for i := range cloned {
		cloned[i].FailureClassCounts = slices.Clone(cloned[i].FailureClassCounts)
	}
	return cloned
}

func renderRegistryCollectorLines(rows []RegistryCollectorSnapshot) []string {
	if len(rows) == 0 {
		return nil
	}
	lines := []string{"Registry collectors:"}
	for _, row := range rows {
		line := fmt.Sprintf(
			"  %s instances=%d active_scopes=%d completed_generations=%d retryable_failures=%d terminal_failures=%d",
			row.CollectorKind,
			row.ConfiguredInstances,
			row.ActiveScopes,
			row.CompletedGenerations,
			row.RetryableFailures,
			row.TerminalFailures,
		)
		if !row.LastCompletedAt.IsZero() {
			line += fmt.Sprintf(" last_completed_at=%s", row.LastCompletedAt.UTC().Format(time.RFC3339))
		}
		if len(row.FailureClassCounts) > 0 {
			line += fmt.Sprintf(" failure_classes=%s", formatNamedTotals(toCountMap(row.FailureClassCounts)))
		}
		lines = append(lines, line)
	}
	return lines
}
