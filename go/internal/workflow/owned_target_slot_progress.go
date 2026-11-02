// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"slices"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// OwnedPackageVirtualSlotCount is V, the pinned virtual-slot modulus the OSV
// slot scopes (#9432) and the persisted slot progress (#9776) share. It is a
// code constant on purpose: the reader's hash-slot count P follows the live
// target total and oscillates at power-of-two boundaries, while a virtual slot
// is a stable identity. Every P that is at most V divides V, so a progress row
// keyed on a virtual slot survives a change of P. A window holds whole virtual
// slots only while P <= V.
const OwnedPackageVirtualSlotCount = 1024

// SlotDerivationKindVulnerability is the derivation_kind of the progress rows
// the OSV owned-package derivation writes. The package-registry planner reads
// the same reader and may opt in later under its own kind.
const SlotDerivationKindVulnerability = "vulnerability"

// Slot selection outcomes (#9776). The set is closed: it labels the
// eshu_dp_owned_target_slot_selection_total counter, so every value must stay
// low-cardinality and none may carry an identifier.
const (
	// SlotSelectionOldest means the read chose the V/P least recently planned
	// virtual slots.
	SlotSelectionOldest = "oldest"
	// SlotSelectionSameBucketReuse means the bucket already had recorded slots,
	// so a repeat reconcile reproduced the same plan.
	SlotSelectionSameBucketReuse = "same_bucket_reuse"
	// SlotSelectionClockFallback means P exceeds V, so the window cannot hold
	// whole virtual slots and the read used the clock slot, exactly as today.
	SlotSelectionClockFallback = "clock_fallback"
	// SlotSelectionEmptySlot means the picked slots held no work, and progress
	// advanced for them anyway so they cannot pin the head.
	SlotSelectionEmptySlot = "empty_slot"
	// SlotSelectionTruncated means the read limit cut the picked slots short.
	SlotSelectionTruncated = "truncated"
	// SlotSelectionAdvanced means the admission transaction moved progress.
	SlotSelectionAdvanced = "advanced"
	// SlotSelectionLostRace means another coordinator advanced progress for the
	// bucket first, so this pass moved nothing.
	SlotSelectionLostRace = "lost_race"
)

// OwnedTargetSlotSelector turns on progress-ordered virtual-slot selection for
// one owned-package read. The zero value keeps the wall-clock slot, which is
// the rollback state (#9776).
type OwnedTargetSlotSelector struct {
	// CollectorInstanceID scopes the progress rows to one collector instance.
	CollectorInstanceID string
	// DerivationKind scopes the progress rows to one derivation, for example
	// [SlotDerivationKindVulnerability].
	DerivationKind string
	// Interval is the reconcile bucket width. The read multiplies bucket ages
	// by it to report the oldest planned slot age in seconds.
	Interval time.Duration
}

// Enabled reports whether the read should select by persisted progress.
func (s OwnedTargetSlotSelector) Enabled() bool {
	return s.CollectorInstanceID != "" && s.DerivationKind != ""
}

// OwnedTargetSlotSelection is what a progress-ordered read chose and what the
// admission step must advance afterwards. The zero value means the read was
// not progress-ordered.
type OwnedTargetSlotSelection struct {
	// Mode is one of [SlotSelectionOldest], [SlotSelectionSameBucketReuse], or
	// [SlotSelectionClockFallback]; empty when selection was not enabled.
	Mode string
	// PickedSlots are the chosen virtual slots in ascending order, empty in
	// clock-fallback mode.
	PickedSlots []int
	// AdvanceSlots are the picked slots the read covered in full (or the head
	// slot that alone exceeded the limit). Only these may be advanced.
	AdvanceSlots []int
	// Truncated is set when the read did not cover every picked slot in full:
	// the reader cut the window, or it returned more rows than the target limit,
	// so the derivation drops the lookahead row as budget exhausted.
	Truncated bool
	// HeadTruncated is set when the head slot alone exceeded the read limit and
	// was advanced anyway so it cannot pin the instance.
	HeadTruncated bool
	// ProgressRows is how many virtual slots have a progress row; below V the
	// first rotation is still warming up.
	ProgressRows int64
	// OldestPlannedBucket is the smallest last_planned_bucket on file, valid
	// when ProgressRows is positive.
	OldestPlannedBucket int64
	// OldestSlotAge is (bucket - OldestPlannedBucket) times the interval,
	// valid when ProgressRows is positive.
	OldestSlotAge time.Duration
}

// Warm reports whether every virtual slot has been planned at least once, the
// condition for the oldest-slot-age gauge to mean a rotation age.
func (s OwnedTargetSlotSelection) Warm() bool {
	return s.ProgressRows >= OwnedPackageVirtualSlotCount
}

// OwnedTargetSlotAdvance is the progress movement an admission transaction
// applies under the per-instance advisory lock (#9776).
type OwnedTargetSlotAdvance struct {
	CollectorKind       scope.CollectorKind
	CollectorInstanceID string
	DerivationKind      string
	// Bucket is the truncated-clock bucket the slots were planned in.
	Bucket int64
	// PlannedAt is the wall clock written beside the bucket.
	PlannedAt time.Time
	// Slots are the virtual slots to move to Bucket, in ascending order.
	Slots []int
}

// SlotAdvanceResult reports how many progress rows a compare-and-set moved.
type SlotAdvanceResult struct {
	// Requested is the number of slots the transaction tried to move.
	Requested int
	// Moved is the number of rows whose last_planned_bucket increased.
	Moved int
}

// LostRace reports whether the transaction asked to move slots and moved none:
// another coordinator already recorded the bucket.
func (r SlotAdvanceResult) LostRace() bool {
	return r.Requested > 0 && r.Moved == 0
}

// VirtualSlotOfKeyHash returns the virtual slot of a target from the low 24
// bits of its sha256 slot key, the same MOD the reader statement applies.
func VirtualSlotOfKeyHash(keyHashLow24 uint32) int {
	return int(keyHashLow24 % OwnedPackageVirtualSlotCount)
}

// ResolveSlotAdvance decides which picked virtual slots a read covered in full
// and so may be advanced. picked and rowSlots are ascending: the reader orders
// rows by virtual slot, so a read cut by its limit cuts whole trailing slots.
//
// A read that was not cut covered every picked slot, including empty ones; an
// empty slot must advance or it is oldest at every tick and the rotation stalls
// (#9776). A read that was cut covered the picked slots below the last slot it
// returned. The last slot returned is partial and stays oldest, except when no
// slot with rows precedes it: that head slot alone exceeds the limit, so
// leaving it would pin the instance, and it advances with headTruncated set.
func ResolveSlotAdvance(picked, rowSlots []int, truncated bool) (advance []int, headTruncated bool) {
	if len(picked) == 0 {
		return nil, false
	}
	if !truncated {
		return slices.Clone(picked), false
	}
	if len(rowSlots) == 0 {
		return nil, false
	}
	last := rowSlots[len(rowSlots)-1]
	for _, slot := range picked {
		if slot < last {
			advance = append(advance, slot)
		}
	}
	if rowSlots[0] == last && slices.Contains(picked, last) {
		advance = append(advance, last)
		headTruncated = true
	}
	return advance, headTruncated
}
