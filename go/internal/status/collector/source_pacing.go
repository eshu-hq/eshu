// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/shared"
)

// SourcePacingSnapshot publishes one source system's per-source reducer
// backpressure state (#10882 AC6): whether intake from the source is
// slowed, why, and the claimable reducer observation behind the verdict.
// It carries only source identity and bounded queue numbers, so the
// operator status surface stays credential-safe like BackpressureSnapshot.
type SourcePacingSnapshot struct {
	SourceSystem string
	Slowed       bool
	// Reason names why intake is slowed (source_behind). Empty unless
	// Slowed.
	Reason string
	// OldestClaimableAge is the age of the source's oldest claimable
	// outstanding reducer row at read time.
	OldestClaimableAge time.Duration
	// ClaimableDepth counts the source's claimable outstanding reducer
	// rows at read time.
	ClaimableDepth int64
	// EngagedSince is when the current slowed engagement began. Zero
	// unless Slowed.
	EngagedSince time.Time
	// Stuck reports an engagement older than the configured bound with
	// the queue still behind: still slowed, and loud. It is evaluated
	// by the report layer, which holds the operator thresholds; the
	// store snapshot leaves it false.
	Stuck bool
}

// CloneSourcePacing copies source pacing rows in stable source order with
// non-negative ages, matching the clone discipline of the other status
// projections.
func CloneSourcePacing(rows []SourcePacingSnapshot) []SourcePacingSnapshot {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]SourcePacingSnapshot, 0, len(rows))
	for _, row := range rows {
		row.OldestClaimableAge = shared.NonNegativeDuration(row.OldestClaimableAge)
		cloned = append(cloned, row)
	}
	slices.SortFunc(cloned, func(a, b SourcePacingSnapshot) int {
		return strings.Compare(a.SourceSystem, b.SourceSystem)
	})
	return cloned
}

// RenderSourcePacingLines renders the per-source slowed state for the text
// status surface: one line per source naming slowed state, reason, and the
// observation behind it.
func RenderSourcePacingLines(rows []SourcePacingSnapshot) []string {
	if len(rows) == 0 {
		return nil
	}
	lines := []string{"Source pacing:"}
	for _, row := range CloneSourcePacing(rows) {
		line := fmt.Sprintf(
			"  %s slowed=%t oldest_claimable=%s claimable_depth=%d",
			row.SourceSystem,
			row.Slowed,
			row.OldestClaimableAge,
			row.ClaimableDepth,
		)
		if row.Reason != "" {
			line += " reason=" + row.Reason
		}
		if row.Stuck {
			line += " stuck=true"
		}
		if !row.EngagedSince.IsZero() {
			line += " engaged_since=" + row.EngagedSince.UTC().Format(time.RFC3339)
		}
		lines = append(lines, line)
	}
	return lines
}
