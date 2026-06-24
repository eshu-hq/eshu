// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// CollectorBackpressureSnapshot captures bounded workflow claim pressure for
// one collector family/instance/source-system tuple. It intentionally excludes
// scope ids, source locators, generation ids, payload excerpts, and failure
// messages so the operator status surface stays credential-safe.
type CollectorBackpressureSnapshot struct {
	CollectorKind       string
	CollectorInstanceID string
	SourceSystem        string
	Pending             int
	Claimed             int
	Retrying            int
	DeadLetter          int
	TerminalFailed      int
	Expired             int
	ActiveClaims        int
	OverdueClaims       int
	OldestPendingAge    time.Duration
	OldestRetryAge      time.Duration
	OldestClaimAge      time.Duration
	NextRetryDelay      time.Duration
	FailureClassCounts  []NamedCount
}

func cloneCollectorBackpressure(rows []CollectorBackpressureSnapshot) []CollectorBackpressureSnapshot {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]CollectorBackpressureSnapshot, 0, len(rows))
	for _, row := range rows {
		row.OldestPendingAge = nonNegativeDuration(row.OldestPendingAge)
		row.OldestRetryAge = nonNegativeDuration(row.OldestRetryAge)
		row.OldestClaimAge = nonNegativeDuration(row.OldestClaimAge)
		row.NextRetryDelay = nonNegativeDuration(row.NextRetryDelay)
		row.FailureClassCounts = cloneNamedCounts(row.FailureClassCounts)
		cloned = append(cloned, row)
	}
	slices.SortFunc(cloned, func(a, b CollectorBackpressureSnapshot) int {
		if a.CollectorKind != b.CollectorKind {
			return strings.Compare(a.CollectorKind, b.CollectorKind)
		}
		if a.CollectorInstanceID != b.CollectorInstanceID {
			return strings.Compare(a.CollectorInstanceID, b.CollectorInstanceID)
		}
		return strings.Compare(a.SourceSystem, b.SourceSystem)
	})
	return cloned
}

func cloneNamedCounts(rows []NamedCount) []NamedCount {
	if len(rows) == 0 {
		return nil
	}
	cloned := slices.Clone(rows)
	slices.SortFunc(cloned, func(a, b NamedCount) int {
		return strings.Compare(a.Name, b.Name)
	})
	return cloned
}

func renderCollectorBackpressureLines(rows []CollectorBackpressureSnapshot) []string {
	if len(rows) == 0 {
		return nil
	}
	lines := []string{"Collector backpressure:"}
	for _, row := range rows {
		line := fmt.Sprintf(
			"  %s/%s source_system=%s pending=%d claimed=%d retrying=%d dead_letter=%d terminal=%d expired=%d active_claims=%d overdue_claims=%d oldest_pending=%s oldest_retry=%s oldest_claim=%s next_retry=%s",
			row.CollectorKind,
			row.CollectorInstanceID,
			row.SourceSystem,
			row.Pending,
			row.Claimed,
			row.Retrying,
			row.DeadLetter,
			row.TerminalFailed,
			row.Expired,
			row.ActiveClaims,
			row.OverdueClaims,
			row.OldestPendingAge,
			row.OldestRetryAge,
			row.OldestClaimAge,
			row.NextRetryDelay,
		)
		if classes := formatNamedCounts(row.FailureClassCounts); classes != "" {
			line += " failure_classes=" + classes
		}
		lines = append(lines, line)
	}
	return lines
}

func formatNamedCounts(rows []NamedCount) string {
	if len(rows) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rows))
	for _, row := range cloneNamedCounts(rows) {
		if strings.TrimSpace(row.Name) == "" || row.Count <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d", row.Name, row.Count))
	}
	return strings.Join(parts, ",")
}
