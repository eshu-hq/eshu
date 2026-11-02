// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import (
	"math"
	"time"
)

// DefaultOwnedTargetMaxStaleness is the documented freshness bound for an
// owned-package derivation that sets no max_staleness (#9775): the longest a
// rotation of the owned targets should take. It is a stated contract the
// rotation period is compared with, not a knob that changes planning.
const DefaultOwnedTargetMaxStaleness = 24 * time.Hour

// OwnedTargetFreshness is the rotation period of one owned-package read set
// against the freshness bound. The period is P x interval: the owned-package
// reader plans one hash slot per window and takes P windows to visit every
// slot, so a target waits up to one period for its slot (#9422). Nothing
// persists which slots were planned, so a missed window can add a further
// period; that case is the sibling scheduling issue's, not part of this value.
type OwnedTargetFreshness struct {
	// SlotCount is P, the reader's hash-slot count for this read. Zero or
	// negative means no read happened, and the value is unknown.
	SlotCount int64
	// Interval is the reconcile interval one window spans, after the same
	// blank-interval default the rotation bucket applies.
	Interval time.Duration
	// Period is SlotCount x Interval, saturated at the largest Duration so a
	// huge P never wraps negative and hides a breach.
	Period time.Duration
	// MaxStaleness is the bound the period is compared with.
	MaxStaleness time.Duration
}

// OwnedTargetRotationFreshness derives the rotation period from the slot count
// the reader returned and compares it with maxStaleness. A blank or negative
// interval uses [DefaultReconcileInterval], the default the rotation bucket
// itself applies, and a non-positive maxStaleness uses
// [DefaultOwnedTargetMaxStaleness]. A non-positive slotCount gives an unknown
// value whose [OwnedTargetFreshness.Known] is false.
func OwnedTargetRotationFreshness(slotCount int64, interval, maxStaleness time.Duration) OwnedTargetFreshness {
	if interval <= 0 {
		interval = DefaultReconcileInterval
	}
	if maxStaleness <= 0 {
		maxStaleness = DefaultOwnedTargetMaxStaleness
	}
	freshness := OwnedTargetFreshness{SlotCount: slotCount, Interval: interval, MaxStaleness: maxStaleness}
	if slotCount <= 0 {
		return freshness
	}
	if slotCount > math.MaxInt64/int64(interval) {
		freshness.Period = time.Duration(math.MaxInt64)
		return freshness
	}
	freshness.Period = time.Duration(slotCount) * interval
	return freshness
}

// Known reports whether the read returned a slot count, so Period is real.
func (f OwnedTargetFreshness) Known() bool { return f.SlotCount > 0 }

// Exceeds reports whether the rotation period is longer than the bound. An
// unknown period never exceeds it.
func (f OwnedTargetFreshness) Exceeds() bool { return f.Known() && f.Period > f.MaxStaleness }
