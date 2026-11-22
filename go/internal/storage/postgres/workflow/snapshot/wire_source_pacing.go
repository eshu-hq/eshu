// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package snapshot

import (
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// sectionSourcePacing stores one per-source reducer backpressure row
// (#10882 AC6). Without it the stored-summary path dropped the whole
// source_pacing list, so a deployment reading the summary never saw which
// source was slowed.
const sectionSourcePacing = "source_pacing"

// sourcePacingWire is one source_pacing row. Stuck is the writer's verdict:
// it is evaluated where the ESHU_SOURCE_BACKPRESSURE_* thresholds live and
// stored with the row, so a reader without the env still serves it.
type sourcePacingWire struct {
	SourceSystem         string  `json:"source_system"`
	Slowed               bool    `json:"slowed"`
	Reason               string  `json:"reason"`
	OldestClaimableAgeNS int64   `json:"oldest_claimable_age_ns"`
	ClaimableDepth       int64   `json:"claimable_depth"`
	EngagedSince         *string `json:"engaged_since"`
	Stuck                bool    `json:"stuck"`
}

func sourcePacingToWire(row collector.SourcePacingSnapshot) sourcePacingWire {
	return sourcePacingWire{
		SourceSystem: row.SourceSystem, Slowed: row.Slowed, Reason: row.Reason,
		OldestClaimableAgeNS: int64(row.OldestClaimableAge), ClaimableDepth: row.ClaimableDepth,
		EngagedSince: optionalWireTime(row.EngagedSince), Stuck: row.Stuck,
	}
}

func decodeSourcePacing(text string, snap *statuspkg.CoordinatorSnapshot) error {
	var wire sourcePacingWire
	if err := decodeStrict(text, &wire); err != nil {
		return err
	}
	engaged, err := parseOptionalWireTime(wire.EngagedSince)
	if err != nil {
		return err
	}
	snap.SourcePacing = append(snap.SourcePacing, collector.SourcePacingSnapshot{
		SourceSystem: wire.SourceSystem, Slowed: wire.Slowed, Reason: wire.Reason,
		OldestClaimableAge: time.Duration(wire.OldestClaimableAgeNS), ClaimableDepth: wire.ClaimableDepth,
		EngagedSince: engaged, Stuck: wire.Stuck,
	})
	return nil
}
