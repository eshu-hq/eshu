// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"fmt"
	"time"
)

// SourceBackpressureReasonSourceBehind is the slowed-state reason a deferred
// intake reports: the source's oldest claimable reducer work is older than
// the freshness-derived engage age. It doubles as the admission-deferral
// metric reason so slowed intake is countable per source (#10882 AC6).
const SourceBackpressureReasonSourceBehind = "source_behind"

// SourceBackpressureConfig carries the per-source slowed-state thresholds
// (#10882 decision 1). The zero value disables the per-source leg: no source
// ever slows unless an operator configures every threshold and bound. All
// numeric values stay Proposed until slice S0/S1 measurement confirms them;
// nothing in this file ships enforcement defaults.
type SourceBackpressureConfig struct {
	// EngageAge is the oldest-claimable-age above which a source with enough
	// claimable depth slows. Zero disables the whole per-source leg.
	EngageAge time.Duration
	// ReleaseAge is the oldest-claimable-age at or below which a slowed
	// source resumes. The gap between engage and release stops flapping.
	ReleaseAge time.Duration
	// DepthFloor is the minimum claimable depth that can slow a source. One
	// poisoned or deliberately gated old row must never throttle a source.
	DepthFloor int64
	// MaxEngagement bounds one slowed engagement: past it with age still
	// above the release age, the verdict stays slowed and raises Stuck
	// instead of releasing blindly into a saturated source.
	MaxEngagement time.Duration
}

// Enabled reports whether per-source slowing is configured. Every threshold
// and bound must be set: a half-configured throttle is a misconfiguration,
// not a conservative default.
func (c SourceBackpressureConfig) Enabled() bool {
	return c.EngageAge > 0 && c.ReleaseAge > 0 &&
		c.DepthFloor > 0 && c.MaxEngagement > 0
}

// Validate rejects a partially configured leg and any enabled config whose
// release age cannot release: the release age must sit strictly below the
// engage age and no duration or floor may be negative. The disabled zero
// value is valid.
func (c SourceBackpressureConfig) Validate() error {
	if !c.Enabled() {
		if c == (SourceBackpressureConfig{}) {
			return nil
		}
		return fmt.Errorf("source backpressure is partially configured: set every threshold and bound, or none")
	}
	if c.ReleaseAge >= c.EngageAge {
		return fmt.Errorf("source backpressure release age %s must be below engage age %s", c.ReleaseAge, c.EngageAge)
	}
	return nil
}

// SourceQueueSignal is one per-source reducer-queue observation for a
// slowed-state verdict. OldestClaimableAge is the age of the oldest
// claimable outstanding reducer row attributed to the source (active
// generation, pending|retrying, visible now); ClaimableDepth counts those
// rows. Both come from the grouped claimable read the S1 store work owns;
// this package only decides over them and never opens storage.
type SourceQueueSignal struct {
	SourceSystem       string
	OldestClaimableAge time.Duration
	ClaimableDepth     int64
}

// SourceBackpressureLatch is the shared hysteresis bit: whether the source
// is currently slowed. It lives in shared storage alongside the claim path
// (the S1 latch work) so coordinators cannot disagree about a source.
type SourceBackpressureLatch struct {
	Engaged bool
}

// SourceBackpressureDecision is one slowed-state verdict for one source.
type SourceBackpressureDecision struct {
	Slowed bool
	// Reason names why the source slowed and stays set while the latch
	// holds, so the operator can answer "slowed, and why". Empty unless
	// Slowed.
	Reason string
	// SourceSystem echoes the verdict's source so a shared consult cannot
	// misattribute one source's backlog to another.
	SourceSystem string
	// Stuck reports an engagement older than MaxEngagement with age still
	// above the release age: still slowed, and loud.
	Stuck bool
}

// DecideSourceSlowed is the pure slowed-state verdict over one per-source
// observation and the shared latch. It never touches storage.
//
// A disengaged source slows only when its oldest claimable age exceeds the
// engage age and its claimable depth reaches the floor. An engaged source
// releases only when its oldest claimable age falls to the release age, so
// ages between the two follow the latch. A disabled config never slows.
func DecideSourceSlowed(
	cfg SourceBackpressureConfig,
	signal SourceQueueSignal,
	latch SourceBackpressureLatch,
	engagedFor time.Duration,
) SourceBackpressureDecision {
	if !cfg.Enabled() {
		return SourceBackpressureDecision{SourceSystem: signal.SourceSystem}
	}
	behind := signal.OldestClaimableAge > cfg.EngageAge && signal.ClaimableDepth >= cfg.DepthFloor
	if !latch.Engaged {
		if !behind {
			return SourceBackpressureDecision{SourceSystem: signal.SourceSystem}
		}
		return slowed(cfg, signal.SourceSystem, 0)
	}
	if signal.OldestClaimableAge <= cfg.ReleaseAge {
		return SourceBackpressureDecision{SourceSystem: signal.SourceSystem}
	}
	return slowed(cfg, signal.SourceSystem, engagedFor)
}

// slowed builds a slowed verdict. Stuck trips only on a positive overdue
// engagement with age still above the release age: the caller reaches here
// only when the engaged source has not released.
func slowed(cfg SourceBackpressureConfig, sourceSystem string, engagedFor time.Duration) SourceBackpressureDecision {
	return SourceBackpressureDecision{
		Slowed:       true,
		Reason:       SourceBackpressureReasonSourceBehind,
		SourceSystem: sourceSystem,
		Stuck:        engagedFor > cfg.MaxEngagement,
	}
}
