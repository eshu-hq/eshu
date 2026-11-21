// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"time"
)

// SourcePacingJSON is the wire projection of one source's slowed state
// (#10882 AC6). Ages render as both a duration string and seconds, matching
// the BackpressureJSON convention.
type SourcePacingJSON struct {
	SourceSystem              string  `json:"source_system"`
	Slowed                    bool    `json:"slowed"`
	Reason                    string  `json:"reason,omitempty"`
	OldestClaimableAge        string  `json:"oldest_claimable_age"`
	OldestClaimableAgeSeconds float64 `json:"oldest_claimable_age_seconds"`
	ClaimableDepth            int64   `json:"claimable_depth"`
	EngagedSince              string  `json:"engaged_since,omitempty"`
	EngagedDuration           string  `json:"engaged_duration,omitempty"`
	EngagedDurationSeconds    float64 `json:"engaged_duration_seconds,omitempty"`
}

// SourcePacingJSONRows projects source pacing snapshots onto the wire
// shape in stable source order. Engaged durations are measured against the
// snapshot read time so a stale engagement stays visible; unslowed rows
// omit them.
func SourcePacingJSONRows(rows []SourcePacingSnapshot, asOf time.Time) []SourcePacingJSON {
	rows = CloneSourcePacing(rows)
	projected := make([]SourcePacingJSON, 0, len(rows))
	for _, row := range rows {
		entry := SourcePacingJSON{
			SourceSystem:              row.SourceSystem,
			Slowed:                    row.Slowed,
			Reason:                    row.Reason,
			OldestClaimableAge:        row.OldestClaimableAge.String(),
			OldestClaimableAgeSeconds: row.OldestClaimableAge.Seconds(),
			ClaimableDepth:            row.ClaimableDepth,
		}
		if !row.EngagedSince.IsZero() {
			entry.EngagedSince = row.EngagedSince.UTC().Format(time.RFC3339)
			if asOf.After(row.EngagedSince) {
				engaged := asOf.Sub(row.EngagedSince)
				entry.EngagedDuration = engaged.String()
				entry.EngagedDurationSeconds = engaged.Seconds()
			}
		}
		projected = append(projected, entry)
	}
	return projected
}
