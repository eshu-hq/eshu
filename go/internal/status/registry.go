// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// RegistryCollectorSnapshot summarizes claim-driven registry collector runtime
// state without exposing registry hosts, repositories, packages, tags, digests,
// account identifiers, metadata URLs, or credential environment names.
type RegistryCollectorSnapshot struct {
	CollectorKind              string
	ConfiguredInstances        int
	ActiveScopes               int
	RecentCompletedGenerations int
	LastCompletedAt            time.Time
	RetryableFailures          int
	TerminalFailures           int
	FailureClassCounts         []NamedCount
	MetadataTargetCounts       []RegistryMetadataTargetCount
}

// RegistryMetadataTargetCount summarizes package-registry metadata target
// progress by ecosystem without exposing package names or registry URLs.
type RegistryMetadataTargetCount struct {
	Ecosystem   string
	Planned     int
	Completed   int
	Skipped     int
	Stale       int
	Failed      int
	RateLimited int
}

func cloneRegistryCollectorSnapshots(rows []RegistryCollectorSnapshot) []RegistryCollectorSnapshot {
	cloned := slices.Clone(rows)
	for i := range cloned {
		cloned[i].FailureClassCounts = slices.Clone(cloned[i].FailureClassCounts)
		cloned[i].MetadataTargetCounts = slices.Clone(cloned[i].MetadataTargetCounts)
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
			"  %s instances=%d active_scopes=%d recent_completed_generations=%d retryable_failures=%d terminal_failures=%d",
			row.CollectorKind,
			row.ConfiguredInstances,
			row.ActiveScopes,
			row.RecentCompletedGenerations,
			row.RetryableFailures,
			row.TerminalFailures,
		)
		if !row.LastCompletedAt.IsZero() {
			line += fmt.Sprintf(" last_completed_at=%s", row.LastCompletedAt.UTC().Format(time.RFC3339))
		}
		if len(row.FailureClassCounts) > 0 {
			line += fmt.Sprintf(" failure_classes=%s", formatNamedTotals(toCountMap(row.FailureClassCounts)))
		}
		if len(row.MetadataTargetCounts) > 0 {
			line += fmt.Sprintf(" metadata_targets=%s", formatMetadataTargetCounts(row.MetadataTargetCounts))
		}
		lines = append(lines, line)
	}
	return lines
}

func formatMetadataTargetCounts(rows []RegistryMetadataTargetCount) string {
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, fmt.Sprintf(
			"%s(planned=%d completed=%d skipped=%d stale=%d failed=%d rate_limited=%d)",
			row.Ecosystem,
			row.Planned,
			row.Completed,
			row.Skipped,
			row.Stale,
			row.Failed,
			row.RateLimited,
		))
	}
	return strings.Join(parts, " ")
}
